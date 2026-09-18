package tui

import "github.com/charmbracelet/lipgloss"

// PetMood represents the cloud pet's current mood.
type PetMood int

const (
	PetIdle PetMood = iota
	PetRunning
	PetError
	PetDone
)

// Pet holds the cloud pet state.
type Pet struct {
	Mood PetMood
}

func NewPet() *Pet {
	return &Pet{Mood: PetIdle}
}

func (p *Pet) SetMood(mood PetMood) {
	p.Mood = mood
}

// clouds indexed by mood
var cloudFrames = [][]string{
	// Idle ☁️
	{
		`  .--.   `,
		` (    )  `,
		`(_______)`,
	},
	// Running ⛅
	{
		`  .--.   `,
		` (    )☀ `,
		`(_______)`,
	},
	// Error ⛈️
	{
		`  .--. ⚡`,
		` (    )  `,
		`(___)~~~`,
	},
	// Done 🌤️
	{
		`  .--.  ☀`,
		` (    )  `,
		`(_______)`,
	},
}

var moodLabels = []string{"idle", "running", "error", "done"}

func (p *Pet) Render(width int) string {
	frames := cloudFrames[p.Mood]
	label := moodLabels[p.Mood]

	style := stylePet
	switch p.Mood {
	case PetError:
		style = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	case PetRunning:
		style = lipgloss.NewStyle().Foreground(colorHighlight).Bold(true)
	case PetDone:
		style = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	}

	out := ""
	for _, line := range frames {
		out += style.Render(line) + "\n"
	}
	out += styleDim.Render("  " + label)
	_ = width
	return out
}