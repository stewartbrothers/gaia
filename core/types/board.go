package types

// Board is the trimmed view of a kanban board (Aikestra, #404). Stages
// are in board order. Internal IDs are dropped: boards and stages are
// addressed by name (case-insensitive) everywhere gaia accepts one.
type Board struct {
	Name    string  `json:"name"`
	Default bool    `json:"default,omitempty"`
	Paused  bool    `json:"paused,omitempty"`
	Stages  []Stage `json:"stages"`
}

// Stage is one column of a Board. Issues holds the numbers of the open
// Issues in the stage, in queue order; it is populated only when a
// single board is fetched (board list omits it).
type Stage struct {
	Name   string `json:"name"`
	Paused bool   `json:"paused,omitempty"`
	Issues []int  `json:"issues,omitempty"`
}
