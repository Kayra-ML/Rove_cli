package persona

import "testing"

// Every character, category and feature reads in every app language: a new
// catalog entry without its translations fails here, not in front of a user.
func TestCatalogIsTranslated(t *testing.T) {
	for _, c := range Characters {
		for _, lang := range Langs {
			tx, ok := c.I18n[lang]
			if !ok || tx.Name == "" || tx.Summary == "" || tx.Category == "" {
				t.Errorf("character %s has no complete %s text: %+v", c.ID, lang, tx)
			}
		}
	}
	for _, f := range Features {
		for _, lang := range Langs {
			if tx, ok := f.I18n[lang]; !ok || tx.Name == "" || tx.Summary == "" {
				t.Errorf("feature %s has no complete %s text", f.Key, lang)
			}
		}
	}
	// and nothing is translated for an entry that does not exist
	for lang, m := range characterText {
		for id := range m {
			if _, ok := CharacterByID(id); !ok {
				t.Errorf("%s translation for unknown character %q", lang, id)
			}
		}
	}
}
