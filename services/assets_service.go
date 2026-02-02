package services

import (
	"bottrade/database"
	"bottrade/models"
	"context"
	"fmt"
	"log"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
)

type AssetsService struct {
	alpacaClient *AlpacaClient
}

func NewAssetsService() *AssetsService {
	return &AssetsService{
		alpacaClient: GetAlpacaClient(),
	}
}

func (as *AssetsService) SyncAssets() error {
	if as.alpacaClient == nil {
		return fmt.Errorf("Alpaca client not initialized")
	}

	client := as.alpacaClient.GetTradingClient()

	req := alpaca.GetAssetsRequest{
		Status:     "active",
		AssetClass: "us_equity",
	}

	assets, err := client.GetAssets(req)
	if err != nil {
		return fmt.Errorf("failed to fetch assets from Alpaca: %w", err)
	}

	tradableCount := 0
	for _, asset := range assets {
		if !asset.Tradable {
			continue
		}

		_, err := database.DB.Exec(context.Background(),
			`INSERT INTO assets (symbol, name, exchange, tradable)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (symbol) DO UPDATE
			 SET name = EXCLUDED.name,
			     exchange = EXCLUDED.exchange,
			     tradable = EXCLUDED.tradable,
			     updated_at = NOW()`,
			asset.Symbol, asset.Name, asset.Exchange, asset.Tradable)

		if err != nil {
			log.Printf("Warning: Failed to sync asset %s: %v", asset.Symbol, err)
			continue
		}

		tradableCount++
	}

	log.Printf("Assets sync completed: %d tradable US equities synced", tradableCount)
	return nil
}

func (as *AssetsService) GetAssets(limit int, offset int, search string) ([]models.Asset, int, error) {
	ctx := context.Background()

	var countQuery string
	var selectQuery string
	var args []interface{}

	if search != "" {
		countQuery = `SELECT COUNT(*) FROM assets WHERE (symbol ILIKE $1 OR name ILIKE $1) AND tradable = true`
		selectQuery = `SELECT symbol, name, exchange, tradable, updated_at
		               FROM assets
		               WHERE (symbol ILIKE $1 OR name ILIKE $1) AND tradable = true
		               ORDER BY symbol
		               LIMIT $2 OFFSET $3`
		searchPattern := "%" + search + "%"
		args = []interface{}{searchPattern, limit, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM assets WHERE tradable = true`
		selectQuery = `SELECT symbol, name, exchange, tradable, updated_at
		               FROM assets
		               WHERE tradable = true
		               ORDER BY symbol
		               LIMIT $1 OFFSET $2`
		args = []interface{}{limit, offset}
	}

	var totalCount int
	if search != "" {
		err := database.DB.QueryRow(ctx, countQuery, "%"+search+"%").Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to count assets: %w", err)
		}
	} else {
		err := database.DB.QueryRow(ctx, countQuery).Scan(&totalCount)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to count assets: %w", err)
		}
	}

	rows, err := database.DB.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query assets: %w", err)
	}
	defer rows.Close()

	assets := make([]models.Asset, 0)
	for rows.Next() {
		var asset models.Asset
		err := rows.Scan(&asset.Symbol, &asset.Name, &asset.Exchange, &asset.Tradable, &asset.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan asset: %w", err)
		}
		assets = append(assets, asset)
	}

	return assets, totalCount, nil
}
