package whatsapp

import (
	waCompanionReg "go.mau.fi/whatsmeow/proto/waCompanionReg"
	waStore "go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

// ConfigureFullHistorySync asks WhatsApp for a larger initial history sync when
// pairing a fresh companion device. WhatsApp may still cap what it returns.
func ConfigureFullHistorySync(days, sizeMB uint32) {
	waStore.DeviceProps.RequireFullSync = proto.Bool(true)
	if waStore.DeviceProps.HistorySyncConfig == nil {
		waStore.DeviceProps.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{}
	}
	waStore.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(days)
	waStore.DeviceProps.HistorySyncConfig.FullSyncSizeMbLimit = proto.Uint32(sizeMB)
	waStore.DeviceProps.HistorySyncConfig.StorageQuotaMb = proto.Uint32(sizeMB)
	waStore.DeviceProps.HistorySyncConfig.OnDemandReady = proto.Bool(true)
	waStore.DeviceProps.HistorySyncConfig.CompleteOnDemandReady = proto.Bool(true)
}

func backfillStorageChatJID(pendingRequestJID, responseJID string, onDemand bool) string {
	if onDemand && pendingRequestJID != "" {
		return pendingRequestJID
	}
	return responseJID
}
