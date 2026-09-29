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
  文字がラベルにあればエラーにし、呼び出し側がソースを表示する。エンジン自身の配置の
  性質を破る絵も同じで、描くたびに確かめる。線の交差や図の大きさで表示を拒むことはない。

## 対応する図

`flowchart` / `graph`、`sequenceDiagram`、`erDiagram`、`pie`、`stateDiagram` / `stateDiagram-v2`。
範囲は RFP に定める（基準は mermaid の公式ドキュメント、12.0.0）。第 2 段階でこの順に `pie`（済み）、
`stateDiagram`（済み）、`gantt`、`mindmap` を加える。それ以外の種類は「図の種類が未対応」として返す。

円グラフは mermaid と同じように描く — 扇形は書いた順に 12 時から時計回り。全体の 1% 未満の項目は
扇形を描かないが凡例には残す。扇形には全体に対する割合を `toFixed(0)` で丸めて書く。`showData` なら
凡例に値を付ける。違いは 1 つ: すべての項目の % を凡例の右の列にも書き（扇形のない項目は `<1%`）、
扇形の中には収まるときだけ書く（mermaid はすべて扇形の上に書くので、細い扇形では重なって読めない）。

状態遷移図は mermaid と同じように読む — `[*]` はその範囲の開始か終了、20 段まで入れ子にできる複合状態、
`--` の領域、choice・fork・join、注記、範囲ごとの方向。範囲ごとに内側から先に配置する: 複合状態は見出しと、
横に並べた領域を収める枠で、そこへの遷移は枠で止まる。そのため複合状態の枠をまたぐ遷移（外から内側の
状態へ、領域どうし）は未対応で、ソースを表示する。注記は書いた側に置き（上下向きの図では状態の横）、
点線でつなぐ。上下向きの図の開始・終了・choice・fork・join への注記と、自分への遷移がある状態への注記は、
書いた側に置けないので未対応。

## API

```go
d, err := mermaidrender.Parse(src) // src: フェンスの中身
var e *mermaidrender.Error
if errors.As(err, &e) {
	// e.Kind: SyntaxError・UnsupportedType・UnsupportedConstruct（Render だけは
	// LayoutFault も）。e.Line は src の 1 始まりの行番号。どの種類でも
	// 「ソースを表示する」を意味する。
}
switch d := d.(type) {
case *mermaidrender.Flowchart: // Nodes, Links, Subgraphs, Direction
case *mermaidrender.ER:        // Entities（属性つき）, Relationships, Direction
case *mermaidrender.Sequence:  // Participants, Boxes, Events（メッセージ・注記・枠）
case *mermaidrender.Pie:       // Slices（ラベルと値）, ShowData
case *mermaidrender.StateDiagram: // Root: States・Transitions・Notes を持つ範囲。複合状態は Regions を持つ
}

font, err := raster.DefaultFont() // 一度読んで使い回す
// または好きな書体。その書体に無い文字は 1 文字ずつヒラギノで補う
font, err = raster.LoadFont(raster.FontSpec{Path: "/path/Font.ttc", Name: "Font-Regular",
	BoldPath: "/path/Font.ttc", BoldName: "Font-Bold"})
img, err := raster.Render(d, raster.Options{Font: font}) // *image.RGBA。Scale 0 は 2
img, err = raster.RenderSource(src, raster.Options{Font: font})
```

構文の読み取りは mermaid 12.0.0 のドキュメントに従う。ドキュメントが決めていない細部
（ID に使える文字、線の記号の読み方、subgraph の所属）は、同じ版の mermaid 自身の
パーサに従う（記録した例外が 1 つ: `A -- go--> B` を mermaid は「ラベル g と始点の印」と読むが、
ここでは「ラベル go」と読む）。`erDiagram` と `sequenceDiagram` は同じ版の字句解析を規則ごとに移植して読むので、細部まで mermaid と
一致する（`one` や `to` のような語は名前にならない、`direction TD` は 2 つの実体になる）。
`raster` は flowchart と ER 図（実体は表、多重度はカラスの足の記法）を段に分けて配置し、
sequence 図は専用の配置で、白地のカードに描く。PNG への変換と
端末での枠の大きさは呼び出し側が決める。描画が止まらないよう、次を上限とする: ソース 5 万文字（mermaid 自身の maxTextSize）、ノードと
subgraph を合わせて 300（subgraph は 100 まで）、線と関係 500（mermaid 自身の上限で、構文を読む
段階で判定）、ER の実体 300・属性は実体ごとに 200、sequence の参加者 300・イベント 2000・枠の入れ子
50、円グラフの項目 100（値の合計が有限であること）、複合状態の入れ子 20 段、ラベル 1 つあたり 1000 文字、線の長さ 10（mermaid と同じ）、`Scale` 8、1 枚 1200 万画素（時間と
メモリのため）。配置の要素が 2 万を超える flowchart（多くの段をまたぐ長い線）も拒む。PNG の大きさ（1 画素あたり 0.10〜0.67 バイトと幅がある）は呼び出し側が自分の
上限（termimg の 2MiB）で確かめ、超えたらソースを表示する。上限を超えたときは、どのフォントにも無い文字と
同じく `UnsupportedConstruct` のエラーになる。

描画のたびに、描く前に配置を確かめる: 箱どうしが重ならない・ラベルは自分の箱に収まり他の文字と
重ならない・枠はメンバーを収める・線は両端の輪郭で始まり終わり、他のノードを通らない・矢じりと
カラスの足の後ろには線の区間がある・sequence の矢印は受け手を向き、枠は自分の行を収める。どれかを
破る絵は `LayoutFault` のエラーになる（ソースではなくエンジンの欠陥）。テストが出会わなかった配置の
不具合でも、間違った絵ではなくソースが表示される。この確かめは絵を間違いにするものだけを見る。
テストは同じ性質をより厳しく（間隔・中央揃え・枠の横切り）見るが、それは見た目の問題で、描画を
拒む理由にはしない。上限いっぱいの図でも数十ミリ秒で済む。

## フォント

`FontSpec.Name` は、ファイルの中の書体（.ttc には複数ある）をフル名か PostScript 名で選ぶ。
ファイルに記録されたどの言語の名前でもよく、大文字小文字は区別しない。`HiraginoSans-W6` でも
`ヒラギノ角ゴシック W6` でも選べる。無い名前はエラーになり、ファイルにある書体の一覧が付く。読めない
書体は書体の名前つきで報告する（.ttc は書体ごとに読めたり読めなかったりする）。選んだ書体に無い文字は
1 文字ずつヒラギノ角ゴシックで描き、どの書体にも無い文字（絵文字など）はエラーにする（欠けた絵は
返さない）。インクを残さない字形（x/image が描けない Apple Color Emoji のビットマップなど）も、空白を
除いて「無い文字」とする。フォントのファイルは、通常のファイルで `MaxFontBytes`（256 MiB。最大の
システムフォントは 183 MiB）以下のときだけ読む。異体字セレクタ・ZWJ・ZWNJ・ZWSP は読み飛ばし、どの書体にも無い書式文字・
既定で無視される文字（LRM、word joiner など）も読み飛ばす。1 つの Font を複数の goroutine の描画で
共有してよい（順番に描く）。`mmdpng -font パス -font-name 名前` で試せる。

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
