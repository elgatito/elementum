package xbmc

// EventPlayer ...
type EventPlayer struct {
	handle int
}

// NewEventPlayer ...
func NewEventPlayer() *EventPlayer {
	retVal := -1
	executeJSONRPCEx("EventPlayer_Create", &retVal, nil)
	if retVal < 0 {
		return nil
	}
	return &EventPlayer{
		handle: retVal,
	}
}

// PopEvent ...
func (ep *EventPlayer) PopEvent() string {
	var retVal string
	executeJSONRPCEx("EventPlayer_PopEvent", &retVal, Args{ep.handle})
	return retVal
}

// Clear ...
func (ep *EventPlayer) Clear() {
	retVal := -1
	executeJSONRPCEx("EventPlayer_Clear", &retVal, Args{ep.handle})
}

// IsPlaying ...
func (ep *EventPlayer) IsPlaying() bool {
	retVal := 0
	executeJSONRPCEx("Player_IsPlaying", &retVal, nil)
	return retVal != 0
}

// Close ...
func (ep *EventPlayer) Close() {
	retVal := 0
	executeJSONRPCEx("EventPlayer_Delete", &retVal, Args{ep.handle})
}
