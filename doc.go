// Package mermaidrender reads mermaid source into diagram data that does not
// depend on how it is drawn. The raster package draws that data as an image;
// a text renderer will use the same data later.
//
// The engine draws what it reads and refuses what it cannot read: every
// error means "show the source instead". It never judges whether a diagram
// is pleasant to look at. See docs/en/mermaid-render-rfp.md.
package mermaidrender
