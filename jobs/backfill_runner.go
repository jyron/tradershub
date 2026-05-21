package jobs

import (
	"bottrade/database"
	"bottrade/services"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// backfillTimeout caps a 30-day replay. With ~30 LLM calls × ~5s each plus
// Alpaca fetch, real backfills finish in ~3 min; 10 min is a generous ceiling
// that still trips on a runaway retry storm before the scheduler stalls.
const backfillTimeout = 10 * time.Minute

// pythonDBEnv returns the BOTTRADE_DB env entry the python child needs
// so it opens the same SQLite file the Go server did. Turso (libsql://)
// is unreachable from python; in that case we return "" and the python
// child falls back to its repo-root default (which won't agree with
// production Go state — see plan for the long-term fix: route replay
// trade writes through the HTTP API too).
func pythonDBEnv() string {
	url := strings.TrimSpace(os.Getenv("TURSO_DATABASE_URL"))
	if strings.HasPrefix(url, "file:") {
		return "BOTTRADE_DB=" + strings.TrimPrefix(url, "file:")
	}
	return ""
}

// BackfillRunner polls backfill_jobs for queued work and spawns the python
// adapter to replay 30 days for the bot. One job per tick, sequential — keeps
// Alpaca quota and DB write pressure predictable.
type BackfillRunner struct{}

func NewBackfillRunner() *BackfillRunner { return &BackfillRunner{} }

func (b *BackfillRunner) Name() string                { return "BackfillRunner" }
func (b *BackfillRunner) Interval() time.Duration     { return 60 * time.Second }

// pythonCmd matches the bot scripts' shebang style. Submitters' adapter is
// invoked as a module so it can `from bots import common`.
func pythonCmd() string {
	if p := os.Getenv("BOTTRADE_PYTHON"); p != "" {
		return p
	}
	return "python3"
}

func (b *BackfillRunner) Run() error {
	var jobID, botID string
	var days int
	err := database.DB.QueryRow(
		`SELECT id, bot_id, days_requested
		   FROM backfill_jobs
		  WHERE status = 'queued'
		  ORDER BY requested_at ASC
		  LIMIT 1`,
	).Scan(&jobID, &botID, &days)
	if err != nil {
		// No queued work — completely fine.
		return nil
	}

	// Sanity guard: replay wipes the bot's trades/positions/snapshots
	// (see bots/common.py:622). Don't ever run that against an official bot.
	var tier string
	if err := database.DB.QueryRow(`SELECT COALESCE(tier,'') FROM bots WHERE id = ?`, botID).Scan(&tier); err == nil && tier == "official" {
		_, _ = database.DB.Exec(
			`UPDATE backfill_jobs SET status='failed', error=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
			"refusing to replay an official bot", jobID,
		)
		return fmt.Errorf("backfill blocked: bot %s is official", botID)
	}

	// Load credentials.
	var provider, modelID string
	var baseURL *string
	var encKey, nonce []byte
	var keyVersion int
	err = database.DB.QueryRow(
		`SELECT provider, base_url, model_id, encrypted_key, nonce, key_version
		   FROM bot_credentials WHERE bot_id = ?`,
		botID,
	).Scan(&provider, &baseURL, &modelID, &encKey, &nonce, &keyVersion)
	if err != nil {
		_, _ = database.DB.Exec(
			`UPDATE backfill_jobs SET status='failed', error=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
			"credentials not found", jobID,
		)
		return fmt.Errorf("backfill %s: credentials missing for bot %s: %w", jobID, botID, err)
	}

	llmKey, err := services.Vault().Decrypt(encKey, nonce, keyVersion)
	if err != nil {
		_, _ = database.DB.Exec(
			`UPDATE backfill_jobs SET status='failed', error=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
			"failed to decrypt credentials: "+err.Error(), jobID,
		)
		return fmt.Errorf("backfill %s decrypt: %w", jobID, err)
	}

	if _, err := database.DB.Exec(
		`UPDATE backfill_jobs SET status='running', started_at=CURRENT_TIMESTAMP WHERE id=?`,
		jobID,
	); err != nil {
		return err
	}

	module := adapterModuleFor(provider)
	if module == "" {
		_, _ = database.DB.Exec(
			`UPDATE backfill_jobs SET status='failed', error=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
			"no adapter for provider "+provider, jobID,
		)
		return fmt.Errorf("backfill %s: no adapter for provider %s", jobID, provider)
	}

	log.Printf("BackfillRunner: spawning %s for bot %s (%s, %d days)", module, botID, modelID, days)

	ctx, cancel := context.WithTimeout(context.Background(), backfillTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, pythonCmd(), "-m", module, "--replay", fmt.Sprintf("%d", days))
	cmd.Env = append(os.Environ(),
		"BOT_ID="+botID,
		"BOT_LLM_API_KEY="+string(llmKey),
		"BOT_MODEL_ID="+modelID,
		"BACKFILL_JOB_ID="+jobID,
	)
	if dbEnv := pythonDBEnv(); dbEnv != "" {
		cmd.Env = append(cmd.Env, dbEnv)
	}
	if baseURL != nil && *baseURL != "" {
		cmd.Env = append(cmd.Env, "BOT_BASE_URL="+*baseURL)
	}

	out, runErr := cmd.CombinedOutput()
	logTail := tailBytes(out, 4096)

	if runErr != nil {
		errMsg := runErr.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf("timed out after %s", backfillTimeout)
		}
		_, _ = database.DB.Exec(
			`UPDATE backfill_jobs
			    SET status='failed', error=?, log_tail=?, completed_at=CURRENT_TIMESTAMP
			  WHERE id=?`,
			errMsg, logTail, jobID,
		)
		// Bump consecutive errors so a broken submission doesn't loop forever.
		_ = services.RecordError(botID, "backfill failed: "+errMsg)
		log.Printf("BackfillRunner: bot %s FAILED: %s", botID, errMsg)
		return nil
	}

	_, _ = database.DB.Exec(
		`UPDATE backfill_jobs
		    SET status='done', log_tail=?, completed_at=CURRENT_TIMESTAMP, days_done=days_requested
		  WHERE id=?`,
		logTail, jobID,
	)
	_ = services.ClearErrors(botID)
	// Auto-promote to 'verified' once a clean backfill lands — keeps challenger
	// tier reserved for in-flight / failed submissions.
	_, _ = database.DB.Exec(
		`UPDATE bots SET tier='verified' WHERE id=? AND tier='challenger'`,
		botID,
	)
	log.Printf("BackfillRunner: bot %s OK (promoted to verified)", botID)
	return nil
}

// adapterModuleFor maps a credential provider to the python module the
// runner should spawn. Each adapter is a thin env-driven generalization of
// the corresponding hand-written official bot (claude_bot.py, gemini_bot.py,
// grok_bot.py — grok lives under openai_compat with a base_url override).
func adapterModuleFor(provider string) string {
	switch provider {
	case "openai_compat":
		return "bots.openai_compat_bot"
	case "anthropic":
		return "bots.anthropic_compat_bot"
	case "google":
		return "bots.google_compat_bot"
	}
	return ""
}

func tailBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return "…" + string(b[len(b)-n:])
}
