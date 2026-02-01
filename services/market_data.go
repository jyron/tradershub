package services

import (
	"bottrade/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// MarketDataService uses Finnhub.io API for real-time stock quotes
// Free tier: 60 API calls per minute
// Get your API key at: https://finnhub.io/register
type MarketDataService struct {
	apiKey string
	cache  map[string]*cachedQuote
	mu     sync.RWMutex
}

type cachedQuote struct {
	quote     models.Quote
	expiresAt time.Time
}

var marketService *MarketDataService

func InitMarketData(apiKey string) {
	marketService = &MarketDataService{
		apiKey: apiKey,
		cache:  make(map[string]*cachedQuote),
	}
}

func GetMarketService() *MarketDataService {
	return marketService
}

// Finnhub API response structure
// Documentation: https://finnhub.io/docs/api/quote
type finnhubQuote struct {
	CurrentPrice  float64 `json:"c"`  // Current price
	Change        float64 `json:"d"`  // Change
	PercentChange float64 `json:"dp"` // Percent change
	HighPrice     float64 `json:"h"`  // High price of the day
	LowPrice      float64 `json:"l"`  // Low price of the day
	OpenPrice     float64 `json:"o"`  // Open price of the day
	PrevClose     float64 `json:"pc"` // Previous close price
	Timestamp     int64   `json:"t"`  // UNIX timestamp
}

func (s *MarketDataService) GetQuote(symbol string) (*models.Quote, error) {
	// Check cache first (cache for 15 seconds to stay within rate limits)
	s.mu.RLock()
	if cached, ok := s.cache[symbol]; ok && time.Now().Before(cached.expiresAt) {
		s.mu.RUnlock()
		quote := cached.quote
		return &quote, nil
	}
	s.mu.RUnlock()

	// If no API key, return mock data for testing
	if s.apiKey == "" || s.apiKey == "your_api_key_here" {
		return s.getMockQuote(symbol), nil
	}

	// Fetch from Finnhub API
	url := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s", symbol, s.apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch quote from Finnhub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Finnhub API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Finnhub response: %w", err)
	}

	var fhQuote finnhubQuote
	if err := json.Unmarshal(body, &fhQuote); err != nil {
		return nil, fmt.Errorf("failed to parse Finnhub response: %w", err)
	}

	// Check if we got valid data (Finnhub returns 0s for invalid symbols)
	if fhQuote.CurrentPrice == 0 {
		return nil, fmt.Errorf("invalid symbol or no data available from Finnhub: %s", symbol)
	}

	// Parse the response
	quote := s.parseFinnhubQuote(symbol, fhQuote)

	// Cache the quote for 15 seconds
	s.mu.Lock()
	s.cache[symbol] = &cachedQuote{
		quote:     *quote,
		expiresAt: time.Now().Add(15 * time.Second),
	}
	s.mu.Unlock()

	return quote, nil
}

func (s *MarketDataService) parseFinnhubQuote(symbol string, fh finnhubQuote) *models.Quote {
	// Estimate bid/ask spread (0.1% for simplicity)
	// In production, you'd use a separate bid/ask API endpoint
	spread := fh.CurrentPrice * 0.001
	bid := fh.CurrentPrice - spread/2
	ask := fh.CurrentPrice + spread/2

	// Estimate volume from typical trading patterns
	// Finnhub's free quote endpoint doesn't include volume
	// For real volume data, use their /stock/candle endpoint (costs API calls)
	estimatedVolume := int64(10000000) // Placeholder

	return &models.Quote{
		Symbol:        symbol,
		Price:         fh.CurrentPrice,
		Bid:           bid,
		Ask:           ask,
		Volume:        estimatedVolume,
		Change:        fh.Change,
		ChangePercent: fh.PercentChange,
		Timestamp:     time.Unix(fh.Timestamp, 0),
	}
}

// Mock data for testing when API key is not set
// This returns fake prices - DO NOT USE IN PRODUCTION
func (s *MarketDataService) getMockQuote(symbol string) *models.Quote {
	// Simple mock prices based on symbol hash
	basePrice := 100.0 + float64(len(symbol)*10)
	spread := basePrice * 0.001

	return &models.Quote{
		Symbol:        symbol,
		Price:         basePrice,
		Bid:           basePrice - spread/2,
		Ask:           basePrice + spread/2,
		Volume:        1000000,
		Change:        2.30,
		ChangePercent: 1.31,
		Timestamp:     time.Now(),
	}
}
