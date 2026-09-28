package mermaidrender

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The cases quote the mermaid 12.0.0 documentation
// (syntax/entityRelationshipDiagram.md) section by section; where the
// documentation is silent, erDiagram.jison and erDb.ts of the same version
// decide, and the case says so.

// dumpER writes an ER diagram compactly:
//
//	entity name "label"
//	  attr type name keys "comment"
//	rel from -> to fromCard/toCard identifying|non-identifying "label"
func dumpER(d *ER) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dir %s\n", d.Direction)
	for _, e := range d.Entities {
		fmt.Fprintf(&b, "entity %s %q\n", e.Name, e.Label)
		for _, a := range e.Attributes {
			fmt.Fprintf(&b, "  attr %s %s", a.Type, a.Name)
			if len(a.Keys) > 0 {
				fmt.Fprintf(&b, " %s", strings.Join(a.Keys, ","))
			}
			if a.Comment != "" {
				fmt.Fprintf(&b, " %q", a.Comment)
			}
			b.WriteString("\n")
		}
	}
	for _, r := range d.Relationships {
		kind := "identifying"
		if !r.Identifying {
			kind = "non-identifying"
		}
		fmt.Fprintf(&b, "rel %s -> %s %s/%s %s %q\n", r.From.Name, r.To.Name, r.FromCard, r.ToCard, kind, r.Label)
	}
	return b.String()
}

func mustER(t *testing.T, src string) *ER {
	t.Helper()
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	e, ok := d.(*ER)
	if !ok {
		t.Fatalf("Parse(%q) = %T, want *ER", src, d)
	}
	return e
}

func TestERDocumentation(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"intro: order example", `---
title: Order example
---
erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE-ITEM : contains
    CUSTOMER }|..|{ DELIVERY-ADDRESS : uses`, `dir TB
entity CUSTOMER "CUSTOMER"
entity ORDER "ORDER"
entity LINE-ITEM "LINE-ITEM"
entity DELIVERY-ADDRESS "DELIVERY-ADDRESS"
rel CUSTOMER -> ORDER exactly-one/zero-or-more identifying "places"
rel ORDER -> LINE-ITEM exactly-one/one-or-more identifying "contains"
rel CUSTOMER -> DELIVERY-ADDRESS one-or-more/one-or-more non-identifying "uses"
`},
		{"intro: attributes", `erDiagram
    CUSTOMER ||--o{ ORDER : places
    CUSTOMER {
        string name
        string custNumber
        string sector
    }
    ORDER {
        int orderNumber
        string deliveryAddress
    }`, `dir TB
entity CUSTOMER "CUSTOMER"
  attr string name
  attr string custNumber
  attr string sector
entity ORDER "ORDER"
  attr int orderNumber
  attr string deliveryAddress
rel CUSTOMER -> ORDER exactly-one/zero-or-more identifying "places"
`},
		{"entities and relationships", `erDiagram
    PROPERTY ||--|{ ROOM : contains`, `dir TB
entity PROPERTY "PROPERTY"
entity ROOM "ROOM"
rel PROPERTY -> ROOM exactly-one/one-or-more identifying "contains"
`},
		{"only the first entity is mandatory", `erDiagram
    CUSTOMER`, `dir TB
entity CUSTOMER "CUSTOMER"
`},
		{"unicode text", `erDiagram
    "This ❤ Unicode"`, `dir TB
entity This ❤ Unicode "This ❤ Unicode"
`},
		{"cardinality symbols, left and right", `erDiagram
    A |o--o| B : zo
    C ||--|| D : ex
    E }o--o{ F : zm
    G }|--|{ H : om`, `dir TB
entity A "A"
entity B "B"
entity C "C"
entity D "D"
entity E "E"
entity F "F"
entity G "G"
entity H "H"
rel A -> B zero-or-one/zero-or-one identifying "zo"
rel C -> D exactly-one/exactly-one identifying "ex"
rel E -> F zero-or-more/zero-or-more identifying "zm"
rel G -> H one-or-more/one-or-more identifying "om"
`},
		{"cardinality aliases", `erDiagram
    A one or zero to zero or one B : x
    C one or more to one or many D : x
    E many(1) to 1+ F : x
    G zero or more to zero or many H : x
    I many(0) to 0+ J : x
    K only one to 1 L : x`, `dir TB
entity A "A"
entity B "B"
entity C "C"
entity D "D"
entity E "E"
entity F "F"
entity G "G"
entity H "H"
entity I "I"
entity J "J"
entity K "K"
entity L "L"
rel A -> B zero-or-one/zero-or-one identifying "x"
rel C -> D one-or-more/one-or-more identifying "x"
rel E -> F one-or-more/one-or-more identifying "x"
rel G -> H zero-or-more/zero-or-more identifying "x"
rel I -> J zero-or-more/zero-or-more identifying "x"
rel K -> L exactly-one/exactly-one identifying "x"
`},
		{"identification", `erDiagram
    CAR ||--o{ NAMED-DRIVER : allows
    PERSON }o..o{ NAMED-DRIVER : is`, `dir TB
entity CAR "CAR"
entity NAMED-DRIVER "NAMED-DRIVER"
entity PERSON "PERSON"
rel CAR -> NAMED-DRIVER exactly-one/zero-or-more identifying "allows"
rel PERSON -> NAMED-DRIVER zero-or-more/zero-or-more non-identifying "is"
`},
		{"identification aliases", `erDiagram
    CAR 1 to zero or more NAMED-DRIVER : allows
    PERSON many(0) optionally to 0+ NAMED-DRIVER : is`, `dir TB
entity CAR "CAR"
entity NAMED-DRIVER "NAMED-DRIVER"
entity PERSON "PERSON"
rel CAR -> NAMED-DRIVER exactly-one/zero-or-more identifying "allows"
rel PERSON -> NAMED-DRIVER zero-or-more/zero-or-more non-identifying "is"
`},
		{"jison: the other dashed forms .- and -.", `erDiagram
    A ||.-o{ B : x
    C ||-.o{ D : y`, `dir TB
entity A "A"
entity B "B"
entity C "C"
entity D "D"
rel A -> B exactly-one/zero-or-more non-identifying "x"
rel C -> D exactly-one/zero-or-more non-identifying "y"
`},
		{"attribute keys and comments", `erDiagram
    CAR ||--o{ NAMED-DRIVER : allows
    CAR {
        string registrationNumber PK
        string make
        string model
        string[] parts
    }
    PERSON ||--o{ NAMED-DRIVER : is
    PERSON {
        string driversLicense PK "The license #"
        string(99) firstName "Only 99 characters are allowed"
        string lastName
        string phone UK
        int age
    }
    NAMED-DRIVER {
        string carRegistrationNumber PK, FK
        string driverLicence PK, FK
    }
    MANUFACTURER only one to zero or more CAR : makes`, `dir TB
entity CAR "CAR"
  attr string registrationNumber PK
  attr string make
  attr string model
  attr string[] parts
entity NAMED-DRIVER "NAMED-DRIVER"
  attr string carRegistrationNumber PK,FK
  attr string driverLicence PK,FK
entity PERSON "PERSON"
  attr string driversLicense PK "The license #"
  attr string(99) firstName "Only 99 characters are allowed"
  attr string lastName
  attr string phone UK
  attr int age
entity MANUFACTURER "MANUFACTURER"
rel CAR -> NAMED-DRIVER exactly-one/zero-or-more identifying "allows"
rel PERSON -> NAMED-DRIVER exactly-one/zero-or-more identifying "is"
rel MANUFACTURER -> CAR exactly-one/zero-or-more identifying "makes"
`},
		{"optional attribute types", `erDiagram
    PERSON {
        string firstName
        string? middleName
        string lastName
    }`, `dir TB
entity PERSON "PERSON"
  attr string firstName
  attr string? middleName
  attr string lastName
`},
		{"entity name aliases", `erDiagram
    p[Person] {
        string firstName
        string lastName
    }
    a["Customer Account"] {
        string email
    }
    p ||--o| a : has`, `dir TB
entity p "Person"
  attr string firstName
  attr string lastName
entity a "Customer Account"
  attr string email
rel p -> a exactly-one/zero-or-one identifying "has"
`},
		{"direction LR", `erDiagram
    direction LR
    CUSTOMER ||--o{ ORDER : places`, `dir LR
entity CUSTOMER "CUSTOMER"
entity ORDER "ORDER"
rel CUSTOMER -> ORDER exactly-one/zero-or-more identifying "places"
`},
		{"direction BT and RL, any case", `erDiagram
    direction bt
    A
    direction RL`, `dir RL
entity A "A"
`},
		{"styling a node: ignored", `erDiagram
    id1||--||id2 : label
    style id1 fill:#f9f,stroke:#333,stroke-width:4px
    style id2 fill:#bbf,stroke:#f66,stroke-width:2px,color:#fff,stroke-dasharray: 5 5`, `dir TB
entity id1 "id1"
entity id2 "id2"
rel id1 -> id2 exactly-one/exactly-one identifying "label"
`},
		{"classes: ignored", `erDiagram
    direction TB
    CAR:::someclass {
        string make
    }
    HOUSE:::someclass
    PERSON:::foo ||--|| CAR : owns
    PERSON o{--|| HOUSE:::bar : has
    classDef someclass fill:#f96
    classDef firstClassName,secondClassName font-size:12pt
    class CAR,HOUSE foo,bar
    classDef default fill:#f9f,stroke:#333,stroke-width:4px;`, `dir TB
entity CAR "CAR"
  attr string make
entity HOUSE "HOUSE"
entity PERSON "PERSON"
rel PERSON -> CAR exactly-one/exactly-one identifying "owns"
rel PERSON -> HOUSE zero-or-more/exactly-one identifying "has"
`},
		{"erDb: blocks for one entity add up, the first alias stays", `erDiagram
    A[First] {
        int x
    }
    A[Second] {
        int y
    }`, `dir TB
entity A "First"
  attr int x
  attr int y
`},
		{"common.ts: generics with ~", `erDiagram
    A {
        List~int~ items
        Map~K,V~ pairs
    }`, `dir TB
entity A "A"
  attr List<int> items
  attr Map<K,V> pairs
`},
		{"entity codes in a name and a label", `erDiagram
    "A #quot;quoted#quot; name" ||--o{ B : "a #35;1 label"`, `dir TB
entity A #quot;quoted#quot; name "A \"quoted\" name"
entity B "B"
rel A #quot;quoted#quot; name -> B exactly-one/zero-or-more identifying "a #1 label"
`},
		{"jison: keywords win, case-insensitively", `erDiagram
    ORDERS ONE TO MANY ITEMS : has`, `dir TB
entity ORDERS "ORDERS"
entity ITEMS "ITEMS"
rel ORDERS -> ITEMS exactly-one/zero-or-more identifying "has"
`},
		{"jison: two names on a line are two entities", `erDiagram
    CUSTOMER ORDER`, `dir TB
entity CUSTOMER "CUSTOMER"
entity ORDER "ORDER"
`},
		{"jison: an unquoted label is one word; the next is an entity", `erDiagram
    A ||--o{ B : places order`, `dir TB
entity A "A"
entity B "B"
entity order "order"
rel A -> B exactly-one/zero-or-more identifying "places"
`},
		{"jison: direction TD is not a direction (two entities)", `erDiagram
    direction TD`, `dir TB
entity direction "direction"
entity TD "TD"
`},
		{"jison: a line mentioning direction TB is a direction statement", `erDiagram
    A ||--o{ B : "direction TB"`, `dir TB
`},
		{"jison: keywords only as whole words", `erDiagram
    Toaster ||--o{ Endless : x
    Oneness ||--o{ Manyfold : y
    Classic ||--o{ Styles : z`, `dir TB
entity Toaster "Toaster"
entity Endless "Endless"
entity Oneness "Oneness"
entity Manyfold "Manyfold"
entity Classic "Classic"
entity Styles "Styles"
rel Toaster -> Endless exactly-one/zero-or-more identifying "x"
rel Oneness -> Manyfold exactly-one/zero-or-more identifying "y"
rel Classic -> Styles exactly-one/zero-or-more identifying "z"
`},
		{"jison: a key only as a whole word", `erDiagram
    A {
        int fk_user FK
        int pkg
    }`, `dir TB
entity A "A"
  attr int fk_user FK
  attr int pkg
`},
		{"jison: the 1 rules and number names", `erDiagram
    A 1--1 B : x
    1 ||--o{ 42 : y`, `dir TB
entity A "A"
entity B "B"
entity 1 "1"
entity 42 "42"
rel A -> B exactly-one/exactly-one identifying "x"
rel 1 -> 42 exactly-one/zero-or-more identifying "y"
`},
		{"jison: \\s is JavaScript's (a full-width space); accTitle ignored", "erDiagram\n    accTitle: Orders\n    direction\u3000LR\n    A", `dir LR
entity A "A"
`},
	}
	for _, c := range cases {
		d := mustER(t, c.src)
		if got := dumpER(d); got != c.want {
			t.Errorf("%s:\n got\n%s\n want\n%s", c.name, got, c.want)
		}
	}
}

func TestERRefusals(t *testing.T) {
	cases := []struct {
		name, src string
		kind      ErrorKind
		line      int
	}{
		{"markdown formatting in a name", `erDiagram
    "This **is** _Markdown_"`, UnsupportedConstruct, 2},
		{"markdown in a label", `erDiagram
    A ||--o{ B : "*very* many"`, UnsupportedConstruct, 2},
		{"subgraphs", `erDiagram
    subgraph title1
        CUSTOMER
    end`, UnsupportedConstruct, 2},
		{"the undocumented u cardinality", `erDiagram
    A u--o{ B : x`, UnsupportedConstruct, 2},
		{"a relationship without a label", `erDiagram
    A ||--o{ B`, SyntaxError, 2},
		{"a label that is not a name or quoted text", `erDiagram
    A ||--o{ B : ,`, SyntaxError, 2},
		{"an attribute type starting with a digit", `erDiagram
    A {
        9int x
    }`, SyntaxError, 3},
		{"an attribute block left open", `erDiagram
    A {
        int x`, SyntaxError, 3},
		{"an attribute named like a key", `erDiagram
    A {
        int pk
    }`, SyntaxError, 3},
		{"an alias in a relationship", `erDiagram
    p[Person] ||--o{ a : x`, SyntaxError, 2},
		{"a lexical error in a style", `erDiagram
    style A stroke-width:1.5px`, SyntaxError, 2},
		{"an entity code inside an attribute word", `erDiagram
    A {
        int x#35;y
    }`, SyntaxError, 3},
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("%s: err = %v, want *Error", c.name, err)
			continue
		}
		if pe.Kind != c.kind || pe.Line != c.line {
			t.Errorf("%s: %v (kind %v line %d), want kind %v line %d", c.name, pe, pe.Kind, pe.Line, c.kind, c.line)
		}
	}
}

func TestERRelationshipCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("erDiagram\n")
	for i := 0; i <= MaxLinks; i++ {
		fmt.Fprintf(&b, "A ||--o{ B%d : x\n", i)
	}
	_, err := Parse(b.String())
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != UnsupportedConstruct {
		t.Fatalf("%d relationships: %v, want an unsupported-construct error", MaxLinks+1, err)
	}
}

func TestERAttributeCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("erDiagram\nA {\n")
	for i := 0; i <= MaxAttributes; i++ {
		fmt.Fprintf(&b, "int a%d\n", i)
	}
	b.WriteString("}\n")
	_, err := Parse(b.String())
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != UnsupportedConstruct {
		t.Fatalf("%d attributes: %v, want an unsupported-construct error", MaxAttributes+1, err)
	}
}

func TestMaxTextSize(t *testing.T) {
	src := "flowchart TD\n" + strings.Repeat("%% padding\n", MaxTextSize/11+1)
	_, err := Parse(src)
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != UnsupportedConstruct {
		t.Fatalf("%d characters: %v, want an unsupported-construct error", len(src), err)
	}
	// Counted as JavaScript counts: an astral character is two.
	if n := utf16Len("a😀"); n != 3 {
		t.Errorf("utf16Len = %d, want 3", n)
	}
}
