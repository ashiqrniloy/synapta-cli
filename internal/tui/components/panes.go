package components

const (
	InputModeChat InputMode = "chat"
	InputModeBash InputMode = "bash"

	inputModeChat InputMode = InputModeChat
	inputModeBash InputMode = InputModeBash

	LayoutModeSplit   LayoutMode = "split"
	LayoutModeStacked LayoutMode = "stacked"

	layoutModeSplit   LayoutMode = LayoutModeSplit
	layoutModeStacked LayoutMode = LayoutModeStacked
)

type InputMode string

func (m InputMode) IsValid() bool {
	switch m {
	case InputModeChat, InputModeBash:
		return true
	default:
		return false
	}
}

type LayoutMode string

func (m LayoutMode) IsValid() bool {
	switch m {
	case LayoutModeSplit, LayoutModeStacked:
		return true
	default:
		return false
	}
}
