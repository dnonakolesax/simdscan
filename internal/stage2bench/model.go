// Package stage2bench contains the generated model used by the JSON stage 2
// prototype benchmarks.
package stage2bench

//go:generate go run github.com/mailru/easyjson/easyjson@v0.9.2 -output_filename model_easyjson.go model.go

//easyjson:json
type SmallFlat struct {
	ID     int64   `json:"id"`
	Count  uint64  `json:"count"`
	Active bool    `json:"active"`
	Score  float64 `json:"score"`
	Level  int64   `json:"level"`
	Ready  bool    `json:"ready"`
	Ratio  float64 `json:"ratio"`
	Code   int64   `json:"code"`
	Size   uint64  `json:"size"`
	Valid  bool    `json:"valid"`
}

//easyjson:json
type LargeStrings struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Active bool    `json:"active"`
	Score  float64 `json:"score"`
	Count  uint64  `json:"count"`
	Note   string  `json:"note"`
}

//easyjson:json
type Numeric struct {
	Signed0   int64   `json:"signed0"`
	Signed1   int64   `json:"signed1"`
	Signed2   int64   `json:"signed2"`
	Signed3   int64   `json:"signed3"`
	Unsigned0 uint64  `json:"unsigned0"`
	Unsigned1 uint64  `json:"unsigned1"`
	Unsigned2 uint64  `json:"unsigned2"`
	Unsigned3 uint64  `json:"unsigned3"`
	Float0    float64 `json:"float0"`
	Float1    float64 `json:"float1"`
	Float2    float64 `json:"float2"`
	Float3    float64 `json:"float3"`
	Bool0     bool    `json:"bool0"`
	Bool1     bool    `json:"bool1"`
	Bool2     bool    `json:"bool2"`
	Bool3     bool    `json:"bool3"`
}

//easyjson:json
type Nested struct {
	ID     int64        `json:"id"`
	Meta   NestedMeta   `json:"meta"`
	Array  [4]int64     `json:"array"`
	Values []float64    `json:"values"`
	Items  []NestedItem `json:"items"`
	Tags   []string     `json:"tags"`
}

//easyjson:json
type NestedMeta struct {
	Version int64 `json:"version"`
	Enabled bool  `json:"enabled"`
}

//easyjson:json
type NestedItem struct {
	Code  int64   `json:"code"`
	Score float64 `json:"score"`
}
