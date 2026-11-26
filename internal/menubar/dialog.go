package menubar

type dialogResponse int

// Dialog presents a default dismissal button and an optional secondary action.
type Dialog struct {
	Title       string
	Message     string
	Dismiss     string
	Action      string
	Destructive bool
}

const (
	dialogDismissed dialogResponse = iota
	dialogAccepted
	dialogBusy
)
