// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package models

import (
	"encoding/json"
	itemA "example.com/gomodel-test/a/v1"
	itemB "example.com/gomodel-test/b/v1"
	"example.com/gomodel-test/external"
	"time"
)

type unexportedEmbedded struct {
	Promoted string `json:"promoted"`
	Clash    string `json:"clash"`
}
type PointerEmbedded struct {
	PointerPromoted string  `json:"pointerPromoted"`
	Optional        *string `json:"optional"`
}
type TaggedEmbedded struct {
	Nested string `json:"nested"`
}
type Left struct {
	Ambiguous string `json:"ambiguous"`
	Tagged    string `json:"Winner"`
}
type Right struct {
	Ambiguous string `json:"ambiguous"`
	Winner    string
}
type Mode string

const (
	ModeOn  Mode = "on"
	ModeOff Mode = "off"
)

type Number int64

const (
	NumberOne Number = 1
	NumberTwo Number = 2
)

type PlainName string
type Alias = any

type Parent struct {
	unexportedEmbedded
	TaggedEmbedded `json:"tagged"`
	*PointerEmbedded
	Clash int `json:"clash"`
	Left
	Right
	OmitEmpty       *string            `json:"omitEmpty,omitempty"`
	OmitZero        *string            `json:"omitZero,omitzero"`
	NullablePointer *string            `json:"nullablePointer"`
	NullableSlice   []string           `json:"nullableSlice"`
	NullableMap     map[string]*string `json:"nullableMap"`
	Mode            Mode               `json:"mode"`
	Number          Number             `json:"number"`
	Plain           PlainName          `json:"plain"`
	Anything        Alias              `json:"anything"`
	Inline          struct {
		Child string `json:"child"`
	} `json:"inline"`
	When  time.Time       `json:"when"`
	Bytes []byte          `json:"bytes"`
	Raw   json.RawMessage `json:"raw"`
}

type Recursive struct {
	Next *Recursive  `json:"next"`
	And  []Recursive `json:"and"`
}
type A struct {
	B *B `json:"b"`
}
type B struct {
	A *A `json:"a"`
}
type UnsupportedOption struct {
	Value int `json:"value,string"`
}
type Custom struct{ Value string }

func (Custom) MarshalJSON() ([]byte, error) { return []byte(`"custom"`), nil }

type UnsupportedMarshaler struct {
	Value Custom `json:"value"`
}

type CustomText string

func (CustomText) MarshalText() ([]byte, error) { return []byte("custom"), nil }

type UnsupportedTextMarshaler struct {
	Value CustomText `json:"value"`
}

type PointerSlice struct {
	Values *[]string `json:"values,omitempty"`
}

type PointerPresence struct {
	Bool   *bool     `json:"bool,omitempty"`
	Number *int      `json:"number,omitempty"`
	Text   *string   `json:"text,omitzero"`
	Values *[]string `json:"values,omitempty"`
}

type CommaTag struct {
	Value string `json:"v,"`
}

type Page[T any] struct {
	Item T `json:"item"`
}
type Foo struct {
	Name string `json:"name"`
}
type Bar struct {
	Number int `json:"number"`
}
type GenericPages struct {
	Foo Page[Foo] `json:"foo"`
	Bar Page[Bar] `json:"bar"`
}

type ExternalRecursive struct {
	Root external.Node `json:"root"`
}

type PromotedTime struct{ time.Time }
type InlinePromotedTime struct {
	Value struct{ time.Time } `json:"value"`
}

type DeepZ struct {
	Z int `json:"z"`
}
type SharedMiddle struct{ DeepZ }
type LeftMiddle struct{ SharedMiddle }
type RightMiddle struct{ SharedMiddle }
type CountDepth struct {
	LeftMiddle
	RightMiddle
}

type ExternalSameName struct {
	First  itemA.Item `json:"first"`
	Second itemB.Item `json:"second"`
}

type ExternalFirstItem struct {
	First itemA.Item `json:"first"`
}

type SameFields struct {
	Z int `json:"z"`
}
type OtherSameFields SameFields
type DistinctEmbeddings struct {
	SameFields
	OtherSameFields
}

type NamedByte byte
type NamedBytes struct {
	Value []NamedByte `json:"value"`
}

type MarshaledByte byte

func (MarshaledByte) MarshalText() ([]byte, error) { return []byte("marshaled"), nil }

type MarshaledBytes struct {
	Value []MarshaledByte `json:"value"`
}

type JSONByte byte

func (JSONByte) MarshalJSON() ([]byte, error) { return []byte(`"marshaled"`), nil }
func (*JSONByte) UnmarshalJSON([]byte) error  { return nil }

type JSONBytes struct {
	Value []JSONByte `json:"value"`
}
