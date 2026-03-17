package event

func AppInitialized()          {}
func AppExited()               {}
func SessionCreated()          {}
func SessionDeleted()          {}
func SessionSwitched()         {}
func FilePickerOpened()        {}
func PromptSent(_ ...any)      {}
func PromptResponded(_ ...any) {}
func TokensUsed(_ ...any)      {}
func StatsViewed()             {}
