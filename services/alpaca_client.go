package services

import (
	"fmt"
	"log"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
)

type AlpacaClient struct {
	trading    *alpaca.Client
	marketData *marketdata.Client
	paperMode  bool
}

var alpacaClient *AlpacaClient

func InitAlpacaClient(apiKey, secretKey string, paperMode bool) error {
	if apiKey == "" || secretKey == "" {
		return fmt.Errorf("Alpaca API key and secret key are required")
	}

	var baseURL string
	var dataURL string

	if paperMode {
		baseURL = "https://paper-api.alpaca.markets"
		dataURL = "https://data.alpaca.markets"
		log.Println("✓ Alpaca: Paper trading mode (test environment)")
	} else {
		baseURL = "https://api.alpaca.markets"
		dataURL = "https://data.alpaca.markets"
		log.Println("✓ Alpaca: Live trading mode")
	}

	tradingClient := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: secretKey,
		BaseURL:   baseURL,
	})

	marketDataClient := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: secretKey,
		BaseURL:   dataURL,
	})

	alpacaClient = &AlpacaClient{
		trading:    tradingClient,
		marketData: marketDataClient,
		paperMode:  paperMode,
	}

	return nil
}

func GetAlpacaClient() *AlpacaClient {
	return alpacaClient
}

func (ac *AlpacaClient) GetTradingClient() *alpaca.Client {
	return ac.trading
}

func (ac *AlpacaClient) GetMarketDataClient() *marketdata.Client {
	return ac.marketData
}

func (ac *AlpacaClient) IsPaperMode() bool {
	return ac.paperMode
}
