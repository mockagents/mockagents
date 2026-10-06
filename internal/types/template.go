package types

// TemplateFuncNames lists the functions response templates may call
// ({{ uuid }}, {{ fake_name }}, …). The engine's ResponseGenerator defines
// them and the config validator parses templates against this list, so a
// typo'd function or an unclosed action is a validation error instead of a
// 500 on every request. Keep in sync with engine.NewResponseGenerator — the
// engine's TestTemplateFuncNamesMatchGenerator asserts it (the ChaosPresets
// convention).
var TemplateFuncNames = []string{
	"now", "timestamp", "date_offset", "date_format",
	"uuid", "random_int", "random_float", "random_string", "random_choice",
	"upper", "lower", "title",
	"to_json",
	"fake_name", "fake_email", "fake_phone", "fake_company", "fake_username",
}
