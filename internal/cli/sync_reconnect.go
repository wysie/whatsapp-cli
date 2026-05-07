package cli

import (
	"fmt"
	"time"
)

type followReconnectConfig struct {
	Enabled         bool
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	CheckInterval   time.Duration
	StaleEventAfter time.Duration
	MaxAttempts     int
}

func (c followReconnectConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.InitialDelay <= 0 {
		return fmt.Errorf("reconnect initial delay must be greater than zero")
	}
	if c.MaxDelay < c.InitialDelay {
		return fmt.Errorf("reconnect max delay must be >= initial delay")
	}
	if c.CheckInterval <= 0 {
		return fmt.Errorf("reconnect check interval must be greater than zero")
	}
	if c.MaxAttempts < 0 {
		return fmt.Errorf("reconnect max attempts cannot be negative")
	}
	return nil
}

func nextReconnectDelay(attempt int, initial, max time.Duration) time.Duration {
	if attempt <= 1 {
		return initial
	}
	delay := initial
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	return delay
}
