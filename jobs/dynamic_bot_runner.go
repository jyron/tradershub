package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

// liveRunTimeout caps a single --live decision. One LLM call + a few HTTP
// trades; 90s is enough headroom for slow first-token latency without
// letting a hung process freeze the runner goroutine.
const liveRunTimeout = 90 * time.Second

// DynamicBotRunner spawns the live decision for every hosted bot that's
// eligible today. Only touches bots with a row in bot_credentials — the
// original 4 official bots keep running via their own cron.
//
// Sequential by design: Alpaca/Finnhub free-tier quotas are tight and a
// dozen parallel python processes is the fast path to a 429 storm.
type DynamicBotRunner struct{}

func NewDynamicBotRunner() *DynamicBotRunner { return &DynamicBotRunner{} }

func (d *DynamicBotRunner) Name() string            { return "DynamicBotRunner" }
func (d *DynamicBotRunner) Interval() time.Duration { return 5 * time.Minute }

// nyseOpen reports whether US equity markets are currently open. Approximate:
// weekday in America/New_York, 09:30 ≤ wall-clock < 16:00. Holidays are not
// modeled — a bot that runs on a closed day just gets stale prices, which
// the trading engine already handles by no-op'ing trades.
func nyseOpen(now time.Time) bool {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return false
	}
	t := now.In(loc)
	if w := t.Weekday(); w == time.Saturday || w == time.Sunday {
		return false
	}
	mins := t.Hour()*60 + t.Minute()
	return mins >= 9*60+30 && mins < 16*60
}

func (d *DynamicBotRunner) Run() error {
	if !nyseOpen(time.Now()) {
		return nil
	}

	// One spawn per bot per UTC day. Eligible = hosted (has credentials),
	// verified or official, not auto-disabled, hasn't run today.
	rows, err := database.DB.Query(`
		SELECT b.id, c.provider, c.base_url, c.model_id, c.encrypted_key, c.nonce, c.key_version, COALESCE(b.tier,'')
		  FROM bots b
		  JOIN bot_credentials c ON c.bot_id = b.id
		 WHERE b.tier IN ('verified','official')
		   AND b.is_active = 1
		   AND b.claimed = 1
		   AND (b.disabled_reason IS NULL OR b.disabled_reason = '')
		   AND COALESCE(b.consecutive_errors, 0) < ?
		   AND (b.last_run_at IS NULL OR DATE(b.last_run_at) < DATE('now'))
	`, services.AutoDisableThreshold)
	if err != nil {
		return err
	}
	defer rows.Close()

	type job struct {
		botID, provider, modelID, tier string
		baseURL                        *string
		encKey, nonce                  []byte
		version                        int
	}
	var queue []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.botID, &j.provider, &j.baseURL, &j.modelID, &j.encKey, &j.nonce, &j.version, &j.tier); err != nil {
			continue
		}
		queue = append(queue, j)
	}
	if len(queue) == 0 {
		return nil
	}

	for _, j := range queue {
		// Per-bot LLM-call cap check (skip official). The python loop also
		// increments the counter, but checking here saves a fork.
		capped, err := services.IsCapped(j.botID, "llm_calls", j.tier)
		if err == nil && capped {
			log.Printf("DynamicBotRunner: bot %s skipped (llm_calls cap)", j.botID)
			continue
		}

		llmKey, err := services.Vault().Decrypt(j.encKey, j.nonce, j.version)
		if err != nil {
			log.Printf("DynamicBotRunner: bot %s decrypt failed: %v", j.botID, err)
			_ = services.RecordError(j.botID, "decrypt failed: "+err.Error())
			continue
		}

		module := adapterModuleFor(j.provider)
		if module == "" {
			_ = services.RecordError(j.botID, "no adapter for provider "+j.provider)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), liveRunTimeout)
		cmd := exec.CommandContext(ctx, pythonCmd(), "-m", module, "--live")
		cmd.Env = append(os.Environ(),
			"BOT_ID="+j.botID,
			"BOT_LLM_API_KEY="+string(llmKey),
			"BOT_MODEL_ID="+j.modelID,
		)
		if dbEnv := pythonDBEnv(); dbEnv != "" {
			cmd.Env = append(cmd.Env, dbEnv)
		}
		if j.baseURL != nil && *j.baseURL != "" {
			cmd.Env = append(cmd.Env, "BOT_BASE_URL="+*j.baseURL)
		}
		out, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			errMsg := runErr.Error()
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = fmt.Sprintf("timed out after %s", liveRunTimeout)
			}
			_ = services.RecordError(j.botID, "live run failed: "+errMsg+"; tail: "+tailBytes(out, 1024))
			log.Printf("DynamicBotRunner: bot %s FAILED: %s", j.botID, errMsg)
			continue
		}
		_ = services.ClearErrors(j.botID)
		log.Printf("DynamicBotRunner: bot %s OK", j.botID)
	}
	return nil
}
