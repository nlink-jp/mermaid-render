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
```

構文の読み取りは mermaid 12.0.0 のドキュメントに従う。ドキュメントが決めていない細部
（ID に使える文字、線の記号の読み方、subgraph の所属）は、同じ版の mermaid 自身の
パーサに従う。画像にする側（`raster`）はまだ書いていない。

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
