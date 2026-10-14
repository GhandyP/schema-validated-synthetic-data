package generate

// words is the built-in synthetic word list used by the {word} pattern token.
//
// It is deliberately short and neutral, and it is the honest reason a string
// field containing {word} reads as templated: the wording comes from here rather
// than from a model, because a model in the value path would break the guarantee
// that the same spec and seed produce the same bytes.
var words = []string{
	"alpha", "amber", "basalt", "bravo", "cedar", "coral", "delta", "dune",
	"ember", "falcon", "flint", "gamma", "granite", "harbor", "indigo", "juniper",
	"kilo", "lantern", "maple", "nimbus", "onyx", "pebble", "quartz", "river",
	"sierra", "timber", "umber", "violet", "willow", "xenon", "yarrow", "zephyr",
}

// Words returns a copy of the built-in synthetic word list.
func Words() []string {
	return append([]string(nil), words...)
}
