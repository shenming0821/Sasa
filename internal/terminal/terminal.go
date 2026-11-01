package terminal

const Ghostty = "ghostty"

var Choices = []struct{ ID, Label string }{
	{Ghostty, "Ghostty"},
}

func Normalize(kind string) string {
	return Ghostty
}

func Label(kind string) string {
	for _, choice := range Choices {
		if choice.ID == Normalize(kind) {
			return choice.Label
		}
	}
	return "Ghostty"
}
