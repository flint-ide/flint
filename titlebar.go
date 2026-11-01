package main

// TitlebarMetrics describes where macOS draws the window buttons (all zero on
// other systems, which keep their own window frame). One definition for every
// system, so the generated bindings don't depend on where they are generated.
type TitlebarMetrics struct {
	Height       float64 `json:"height"`       // title bar height in points
	ButtonsLeft  float64 `json:"buttonsLeft"`  // x of the close button
	ButtonsTop   float64 `json:"buttonsTop"`   // y of the close button from the top
	ButtonWidth  float64 `json:"buttonWidth"`  // size of one traffic light
	ButtonHeight float64 `json:"buttonHeight"` //
}
