package tui

// PetMood represents the agent's current activity state.
type PetMood int

const (
	PetIdle PetMood = iota
	PetRunning
	PetError
	PetDone
)

// Pet holds the agent state indicator (replaces the cloud pet with a minimal status dot).
type Pet struct {
	Mood PetMood
}

func NewPet() *Pet {
	return &Pet{Mood: PetIdle}
}

func (p *Pet) SetMood(mood PetMood) {
	p.Mood = mood
}

// Render returns a minimal single-line status indicator for the right rail header.
// Width parameter kept for API compatibility.
func (p *Pet) Render(width int) string {
	_ = width
	switch p.Mood {
	case PetRunning:
		return styleHighlight.Render("● running")
	case PetError:
		return styleError.Render("● error")
	case PetDone:
		return styleSuccess.Render("● done")
	default:
		return styleDim.Render("○ idle")
	}
}