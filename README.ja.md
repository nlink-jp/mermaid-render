# mermaid-render

mermaid のソースを画像にする Go ライブラリ。gem-agent と lagent が、iTerm2 または
kitty の画像方式に対応した端末で、トランスクリプトの mermaid の図を画像として
表示するために使う。

- **自前実装で、組織の外のコードを使わない。** 構文の読み取り・配置・描画はすべて
  ここで書く。mermaid.js は使わない。
- **モデルのツールではなく、ランタイムの描画機能。** ランタイムはトランスクリプトを
  描くときに mermaid のフェンスをその場で画像にする。トランスクリプトには原文のまま
  残る。
- **間違った図は出さず、見た目では拒まない。** 読めない構文や、どのフォントにも無い
  文字がラベルにあればエラーにし、呼び出し側がソースを表示する。線の交差や図の大きさ
  で表示を拒むことはない。

## 対応する図

第 1 段階は `flowchart` / `graph`、`sequenceDiagram`、`erDiagram`。範囲は RFP に
定める（基準は mermaid の公式ドキュメント）。`stateDiagram` と入れ子の subgraph は
第 2 段階。それ以外の種類は「図の種類が未対応」として返す。

## API

```go
d, err := mermaidrender.Parse(src) // src: フェンスの中身
var e *mermaidrender.Error
if errors.As(err, &e) {
	// e.Kind: SyntaxError・UnsupportedType・UnsupportedConstruct。e.Line は src の
	// 1 始まりの行番号。どの種類でも「ソースを表示する」を意味する。
}
f := d.(*mermaidrender.Flowchart) // Nodes, Links, Subgraphs, Direction

font, err := raster.DefaultFont() // 一度読んで使い回す
img, err := raster.Render(d, raster.Options{Font: font}) // *image.RGBA。Scale 0 は 2
img, err = raster.RenderSource(src, raster.Options{Font: font})
```

構文の読み取りは mermaid 12.0.0 のドキュメントに従う。ドキュメントが決めていない細部
（ID に使える文字、線の記号の読み方、subgraph の所属）は、同じ版の mermaid 自身の
パーサに従う。`raster` は flowchart を段に分けて配置し、白地のカードに描く。PNG への変換と
端末での枠の大きさは呼び出し側が決める。描画が止まらないよう、次を上限とする: ノードと
subgraph を合わせて 300（subgraph は 100 まで）、線 500（mermaid 自身の上限で、構文を読む
段階で判定）、ラベル 1 つあたり 1000 文字、線の長さ 10（mermaid と同じ）、`Scale` 8、1 枚 300 万
画素（実測の最悪値 1 画素あたり 0.67 バイトでも PNG は 2MiB 未満。それでも PNG の大きさは呼び出し側で
確かめる）。超えたときは、どのフォントにも無い文字と同じく `UnsupportedConstruct` のエラーになる。

## 依存

`golang.org/x/image`（フォントの読み込み・文字の描画・図形の塗り）。Go チームが保守
するもの。lib-series の「標準ライブラリだけ」という原則の例外で、2026-09-28 に運用者が
承認した。実行時に使う外部のものは macOS のシステムフォントだけで、同梱もダウンロードも
しない。既定のフォントはヒラギノ角ゴシック W3 / W6。

## 開発

```bash
make test     # go test ./...
make vet      # go vet ./...
make build    # dist/mmdpng — 開発用 CLI（mermaid ファイル → PNG）。リリースしない
```

## ドキュメント

- [RFP](docs/ja/mermaid-render-rfp.ja.md) — 仕様、その根拠になった実測、決めたこと
  （[English](docs/en/mermaid-render-rfp.md)）

## ライセンス

MIT
