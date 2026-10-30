//go:build !darwin

package main

// TitlebarMetrics describes where the OS draws window buttons (macOS only).
type TitlebarMetrics struct {
	Height       float64 `json:"height"`
	ButtonsLeft  float64 `json:"buttonsLeft"`
	ButtonsTop   float64 `json:"buttonsTop"`
	ButtonWidth  float64 `json:"buttonWidth"`
	ButtonHeight float64 `json:"buttonHeight"`
}

func styleNativeWindow() {}

func titlebarMetrics() TitlebarMetrics { return TitlebarMetrics{} }
