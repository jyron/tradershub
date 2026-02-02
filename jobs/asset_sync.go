package jobs

import (
	"bottrade/services"
)

type AssetSyncJob struct {
	assetsService *services.AssetsService
}

func NewAssetSyncJob() *AssetSyncJob {
	return &AssetSyncJob{
		assetsService: services.NewAssetsService(),
	}
}

func (j *AssetSyncJob) Name() string {
	return "AssetSync"
}

func (j *AssetSyncJob) Run() error {
	return j.assetsService.SyncAssets()
}
