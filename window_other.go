//go:build !darwin || server

package main

// Without the macOS window (other systems, or the browser-only server build)
// the native tweaks are no-ops.

// TitlebarMetrics describes where the OS draws window buttons (macOS only).
type TitlebarMetrics struct {
	Height       float64 `json:"height"`
	ButtonsLeft  float64 `json:"buttonsLeft"`
	ButtonsTop   float64 `json:"buttonsTop"`
	ButtonWidth  float64 `json:"buttonWidth"`
	ButtonHeight float64 `json:"buttonHeight"`
}

func setNativeAppearance(string) bool { return true }

func systemPrefersDark() bool { return true }

func titlebarMetrics() TitlebarMetrics { return TitlebarMetrics{} }
