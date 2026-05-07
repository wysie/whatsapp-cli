package whatsapp

import (
	"context"
	"os"
	"time"

	"github.com/mdp/qrterminal/v3"
	waHistorySync "go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types/events"
)

// registerHandlers registers event handlers for WhatsApp events.
func (c *Client) registerHandlers() {
	c.WA.AddEventHandler(func(evt interface{}) {
		c.TouchSyncEvent()
		switch v := evt.(type) {
		case *events.Message:
			c.handleMessage(v)
		case *events.HistorySync:
			result := c.handleHistorySync(v)
			if v.Data != nil && v.Data.GetSyncType() == waHistorySync.HistorySync_ON_DEMAND {
				select {
				case c.OnDemandHistorySync <- result:
				default:
				}
			}
			// Check if sync is complete (progress == 100)
			if v.Data != nil && v.Data.Progress != nil && *v.Data.Progress >= 100 {
				c.Logger.Info("history sync complete")
				c.backfillChatNames()
				select {
				case c.SyncComplete <- struct{}{}:
				default:
				}
			}
		case *events.OfflineSyncCompleted:
			c.backfillChatNames()
			select {
			case c.SyncComplete <- struct{}{}:
			default:
			}
		case *events.Connected:
			c.Logger.Info("connected to WhatsApp")
			c.SetSyncMetadata("sync_state", "connected")
			c.SetSyncMetadata("sync_connected_at", nowRFC3339())
			c.SetSyncMetadata("sync_last_error", "")
		case *events.LoggedOut:
			c.Logger.Warn("logged out of WhatsApp")
			c.SetSyncMetadata("sync_state", "logged_out")
			c.SetSyncMetadata("sync_last_error", "logged out of WhatsApp")
		}
	})
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

// ConnectWithQR connects to WhatsApp, displaying a QR code if needed.
func (c *Client) ConnectWithQR(ctx context.Context) error {
	if c.WA.Store.ID == nil {
		qrChan, _ := c.WA.GetQRChannel(ctx)
		if err := c.WA.Connect(); err != nil {
			return err
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stderr)
			} else if evt.Event == "success" {
				break
			}
		}

		return nil
	}

	return c.WA.Connect()
}

// Connect connects to WhatsApp without QR (requires existing session).
func (c *Client) Connect() error {
	return c.WA.Connect()
}
