// Package raster draws a mermaid-render diagram as an image on a white card.
//
// It does not know how large the image will appear: the caller chooses the
// terminal box. Fonts are loaded once by the caller and passed in; this
// package opens no file while drawing unless Options.Font is nil, when
// Render loads the default font for that call.
package raster
