package persona

import (
	"context"
	"errors"
	"strings"

	"github.com/Kayra-ML/rove/internal/provider"
	"github.com/Kayra-ML/rove/internal/store"
	"github.com/Kayra-ML/rove/internal/types"
)

// Resolved is the persona in effect for a session after combining the
// session's own choice, its profile, the character and the default profile.
type Resolved struct {
	SessionID   types.ID `json:"sessionId"`
	Source      string   `json:"source"` // none | character | profile | default-profile | features
	CharacterID string   `json:"characterId,omitempty"`
	ProfileID   types.ID `json:"profileId,omitempty"`
	Name        string   `json:"name,omitempty"`
	// Features is nil when nothing restricts the session (all tools on).
	Features    []string `json:"features"`
	ExtraPrompt string   `json:"extraPrompt,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Model       string   `json:"model,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	// Integrations are the MCP servers the profile may use; nil: all.
	Integrations []string `json:"integrations,omitempty"`
}

// Resolve works out the persona for sessionID.
func Resolve(ctx context.Context, st *store.Store, sessionID types.ID) (Resolved, error) {
	r := Resolved{SessionID: sessionID, Source: "none"}
	sp, err := st.GetSessionPersona(ctx, sessionID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		def, derr := st.GetDefaultProfile(ctx)
		if derr != nil || def == nil {
			return r, derr
		}
		r.fromProfile(*def, nil)
		r.Source = "default-profile"
	case err != nil:
		return r, err
	case sp.ProfileID != "":
		p, perr := st.GetAgentProfile(ctx, sp.ProfileID)
		if perr != nil {
			return r, perr
		}
		r.fromProfile(p, sp.Features)
		r.Source = "profile"
	case sp.CharacterID != "":
		if c, ok := CharacterByID(sp.CharacterID); ok {
			r.CharacterID, r.Name = c.ID, c.Name
			r.Prompt = c.Prompt
			r.Features = pick(sp.Features, c.Features)
			r.Source = "character"
		}
	default:
		if sp.Features != nil {
			r.Features = sp.Features
			r.Source = "features"
		}
	}
	if err == nil {
		r.ExtraPrompt = strings.TrimSpace(sp.ExtraPrompt)
	}
	r.Prompt = compose(r.Prompt, r.Features, r.ExtraPrompt)
	return r, nil
}

func (r *Resolved) fromProfile(p types.AgentProfile, override []string) {
	r.ProfileID, r.Name, r.Model, r.Provider = p.ID, p.Name, p.Model, p.Provider
	r.Integrations = p.Integrations
	var charFeatures []string
	base := ""
	if c, ok := CharacterByID(p.CharacterID); ok {
		r.CharacterID = c.ID
		charFeatures = c.Features
		// "own": the user's prompt stands in for the character's; the
		// character still lends its default permissions
		if p.PromptMode != types.PromptOwn {
			base = c.Prompt
		}
	}
	if extra := strings.TrimSpace(p.SystemPrompt); extra != "" {
		if base != "" {
			base += "\n\n" + extra
		} else {
			base = extra
		}
	}
	r.Prompt = base
	switch {
	case override != nil:
		r.Features = override
	case p.Features != nil:
		r.Features = p.Features
	case charFeatures != nil:
		r.Features = charFeatures
	case base != "":
		r.Features = DefaultFeatures
	}
}

func pick(override, fallback []string) []string {
	if override != nil {
		return override
	}
	if fallback != nil {
		return fallback
	}
	return DefaultFeatures
}

// compose appends the enabled behavior rules and any extra instructions.
func compose(prompt string, features []string, extra string) string {
	var rules []string
	for _, k := range features {
		if f, ok := FeatureByKey(k); ok && f.Rule != "" {
			rules = append(rules, "- "+f.Rule)
		}
	}
	out := strings.TrimSpace(prompt)
	if len(rules) > 0 {
		if out != "" {
			out += "\n\n"
		}
		out += "Working rules:\n" + strings.Join(rules, "\n")
	}
	if extra != "" {
		if out != "" {
			out += "\n\n"
		}
		out += extra
	}
	return out
}

// AllowIntegration reports whether an MCP tool ("mcp_<server>_<tool>")
// comes from a server this persona may use. Other tools pass.
func (r Resolved) AllowIntegration(name string) bool {
	if r.Integrations == nil || !strings.HasPrefix(name, "mcp_") {
		return true
	}
	for _, s := range r.Integrations {
		if strings.TrimSpace(s) != "" && strings.HasPrefix(name, "mcp_"+types.MCPSlug(s)+"_") {
			return true
		}
	}
	return false
}

// AllowTool reports whether a tool may be offered to the model. Tools that
// belong to no known feature stay available, so new tools are not silently
// hidden.

func (r Resolved) AllowTool(name string) bool {
	if r.Features == nil {
		return true
	}
	on := map[string]bool{}
	for _, k := range r.Features {
		on[k] = true
	}
	for _, f := range Features {
		if f.Group != "tools" {
			continue
		}
		owns := f.Prefix != "" && strings.HasPrefix(name, f.Prefix)
		for _, t := range f.Tools {
			if t == name {
				owns = true
			}
		}
		if owns {
			return on[f.Key]
		}
	}
	return true
}

// Cost estimates the tokens the persona adds to every model request: its
// system prompt plus the schemas of the tools it leaves on.
type Cost struct {
	Prompt int `json:"prompt"`
	Tools  int `json:"tools"`
	Total  int `json:"total"`
	// ToolsOff counts tool schemas this persona keeps out of every request.
	ToolsOff int `json:"toolsOff"`
}

func (r Resolved) Cost(specs []provider.ToolSpec) Cost {
	c := Cost{Prompt: tokens(r.Prompt)}
	for _, s := range specs {
		n := tokens(s.Name) + tokens(s.Description) + tokens(string(s.Parameters))
		if r.AllowTool(s.Name) {
			c.Tools += n
		} else {
			c.ToolsOff++
		}
	}
	c.Total = c.Prompt + c.Tools
	return c
}

func tokens(s string) int { return (len([]rune(s)) + 3) / 4 }

// Match finds a character by id, name or tag prefix, for `/character go`.
func Match(q string) (Character, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Character{}, false
	}
	for _, c := range Characters {
		if c.ID == q || strings.ToLower(c.Name) == q {
			return c, true
		}
	}
	for _, c := range Characters {
		if strings.HasPrefix(c.ID, q) || strings.Contains(strings.ToLower(c.Name), q) {
			return c, true
		}
		for _, t := range c.Tags {
			if t == q {
				return c, true
			}
		}
	}
	return Character{}, false
}
