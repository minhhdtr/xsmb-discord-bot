package httpapi

// The wire shapes, mirroring the schemas in contracts/openapi.yaml.
//
// These are written out rather than reusing the domain types directly. A
// domain type changes for domain reasons - a field renamed for clarity, a
// helper added - and none of those should be able to reach a client by
// accident. Keeping a separate layer means the contract only moves when
// somebody edits this file and the YAML beside it, in the same commit.
//
// Dates are ISO strings, not time.Time. Go would marshal a time.Time as
// RFC 3339 with a clock attached, and a draw date has no time of day.

type healthBody struct {
	Status string `json:"status"`
}

type archiveBody struct {
	Draws    int     `json:"draws"`
	Absences int     `json:"absences"`
	Channels int     `json:"channels"`
	Earliest *string `json:"earliest"`
	Latest   *string `json:"latest"`
}

type drawBody struct {
	Date      string   `json:"date"`
	Weekday   string   `json:"weekday"`
	Special   string   `json:"special"`
	De        string   `json:"de"`
	Numbers   []string `json:"numbers"`
	Tails     []string `json:"tails"`
	Table     string   `json:"table"`
	HeadTail  string   `json:"head_tail"`
	Source    string   `json:"source"`
	FetchedAt *string  `json:"fetched_at"`
}

type nhayBody struct {
	Number string `json:"number"`
	Hits   int    `json:"hits"`
}

type dayReportBody struct {
	Date      string     `json:"date"`
	De        string     `json:"de"`
	ChamDau   int        `json:"cham_dau"`
	ChamDuoi  int        `json:"cham_duoi"`
	TongDe    int        `json:"tong_de"`
	Kep       []string   `json:"kep"`
	Nhay      []nhayBody `json:"nhay"`
	Heads     [10]int    `json:"heads"`
	Tails     [10]int    `json:"tails"`
	MuteHeads []int      `json:"mute_heads"`
	MuteTails []int      `json:"mute_tails"`
	TopHeads  []int      `json:"top_heads"`
	TopTails  []int      `json:"top_tails"`
	Table     string     `json:"table"`
}

type ganBody struct {
	Number    string  `json:"number"`
	Days      int     `json:"days"`
	Record    int     `json:"record"`
	NewRecord bool    `json:"new_record"`
	LastSeen  *string `json:"last_seen"`
	RecordEnd *string `json:"record_end"`
}

type frequencyEntry struct {
	Number string `json:"number"`
	Hits   int    `json:"hits"`
	Days   int    `json:"days"`
}

type frequencyListBody struct {
	Grouped bool             `json:"grouped"` // always false
	Days    int              `json:"days"`
	Entries []frequencyEntry `json:"entries"`
}

type bucketBody struct {
	Digit int `json:"digit"`
	Hits  int `json:"hits"`
}

type groupedFrequencyBody struct {
	Grouped  bool         `json:"grouped"` // always true
	Days     int          `json:"days"`
	Group    string       `json:"group"`
	Buckets  []bucketBody `json:"buckets"`
	Total    int          `json:"total"`
	Even     float64      `json:"even"`
	Overlaps bool         `json:"overlaps"`
	Table    string       `json:"table"`
}

type windowBody struct {
	Label string `json:"label"`
	Days  int    `json:"days"`
	Hits  int    `json:"hits"`
	Draws int    `json:"draws"`
	Total int    `json:"total"`
}

type dayHitBody struct {
	Date string `json:"date"`
	Hits int    `json:"hits"`
}

type profileBody struct {
	Number   string       `json:"number"`
	Gan      ganBody      `json:"gan"`
	Windows  []windowBody `json:"windows"`
	Recent   []dayHitBody `json:"recent"`
	AvgCycle float64      `json:"avg_cycle"`
	First    *string      `json:"first"`
	Archive  int          `json:"archive"`
}

type specialDayBody struct {
	Date    string `json:"date"`
	Special string `json:"special"`
	De      string `json:"de"`
}

type specialMonthBody struct {
	Month string           `json:"month"`
	Days  []specialDayBody `json:"days"`
	Table string           `json:"table"`
}

type spinBody struct {
	Numbers []string `json:"numbers"`
	Order   []int    `json:"order"`
}
