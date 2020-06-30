package xbmc

import (
	"time"
)

// DialogProgress ...
type DialogProgress struct {
	hWnd int64
}

// DialogProgressBG ...
type DialogProgressBG struct {
	hWnd int64
}

// OverlayStatus ...
type OverlayStatus struct {
	hWnd int64
}

// DialogInsert ...
func DialogInsert() map[string]string {
	var retVal map[string]string
	executeJSONRPCEx("DialogInsert", &retVal, nil)
	return retVal
}

// NewDialogProgress ...
func NewDialogProgress(title, line1, line2, line3 string) *DialogProgress {
	retVal := int64(-1)
	executeJSONRPCEx("DialogProgress_Create", &retVal, Args{title, line1, line2, line3})
	if retVal < 0 {
		return nil
	}
	return &DialogProgress{
		hWnd: retVal,
	}
}

// Update ...
func (dp *DialogProgress) Update(percent int, line1, line2, line3 string) {
	retVal := -1
	executeJSONRPCEx("DialogProgress_Update", &retVal, Args{dp.hWnd, percent, line1, line2, line3})
}

// IsCanceled ...
func (dp *DialogProgress) IsCanceled() bool {
	retVal := 0
	executeJSONRPCEx("DialogProgress_IsCanceled", &retVal, Args{dp.hWnd})
	return retVal != 0
}

// Close ...
func (dp *DialogProgress) Close() {
	retVal := -1
	executeJSONRPCEx("DialogProgress_Close", &retVal, Args{dp.hWnd})
}

// DialogProgressBGCleanup ...
func DialogProgressBGCleanup() {
	retVal := -1
	executeJSONRPCEx("DialogProgressBG_Cleanup", &retVal, Args{})
}

// NewDialogProgressBG ...
func NewDialogProgressBG(title, message string, translations ...string) *DialogProgressBG {
	retVal := int64(-1)
	executeJSONRPCEx("DialogProgressBG_Create", &retVal, Args{title, message, translations})
	if retVal < 0 {
		return nil
	}
	return &DialogProgressBG{
		hWnd: retVal,
	}
}

// Update ...
func (dp *DialogProgressBG) Update(percent int, heading string, message string) {
	retVal := -1
	executeJSONRPCEx("DialogProgressBG_Update", &retVal, Args{dp.hWnd, percent, heading, message})
}

// IsFinished ...
func (dp *DialogProgressBG) IsFinished() bool {
	retVal := 0
	executeJSONRPCEx("DialogProgressBG_IsFinished", &retVal, Args{dp.hWnd})
	return retVal != 0
}

// Close ...
func (dp *DialogProgressBG) Close() {
	retVal := -1
	executeJSONRPCEx("DialogProgressBG_Close", &retVal, Args{dp.hWnd})
}

// NewOverlayStatus ...
func NewOverlayStatus() *OverlayStatus {
	retVal := int64(-1)
	executeJSONRPCEx("OverlayStatus_Create", &retVal, Args{})
	if retVal < 0 {
		return nil
	}
	return &OverlayStatus{
		hWnd: retVal,
	}
}

// Update ...
func (ov *OverlayStatus) Update(percent int, line1, line2, line3 string) {
	retVal := -1
	executeJSONRPCEx("OverlayStatus_Update", &retVal, Args{ov.hWnd, percent, line1, line2, line3})
}

// Show ...
func (ov *OverlayStatus) Show() {
	retVal := -1
	executeJSONRPCEx("OverlayStatus_Show", &retVal, Args{ov.hWnd})
}

// Hide ...
func (ov *OverlayStatus) Hide() {
	retVal := -1
	executeJSONRPCEx("OverlayStatus_Hide", &retVal, Args{ov.hWnd})
}

// Close ...
func (ov *OverlayStatus) Close() {
	retVal := -1
	executeJSONRPCEx("OverlayStatus_Close", &retVal, Args{ov.hWnd})
}

// Notify ...
func Notify(header string, message string, image string) {
	var retVal string
	executeJSONRPCEx("Notify", &retVal, Args{header, message, image})
}

// InfoLabels ...
func InfoLabels(labels ...string) map[string]string {
	var retVal map[string]string
	executeJSONRPC("XBMC.GetInfoLabels", &retVal, Args{labels})
	return retVal
}

// InfoLabel ...
func InfoLabel(label string) string {
	labels := InfoLabels(label)
	return labels[label]
}

// GetWindowProperty ...
func GetWindowProperty(key string) string {
	var retVal string
	executeJSONRPCEx("GetWindowProperty", &retVal, Args{key})
	return retVal
}

// SetWindowProperty ...
func SetWindowProperty(key string, value string) {
	var retVal string
	executeJSONRPCEx("SetWindowProperty", &retVal, Args{key, value})
}

// Keyboard ...
func Keyboard(args ...interface{}) string {
	var retVal string
	executeJSONRPCEx("Keyboard", &retVal, args)
	return retVal
}

// Dialog ...
func Dialog(title string, message string) bool {
	retVal := 0
	executeJSONRPCEx("Dialog", &retVal, Args{title, message})
	return retVal != 0
}

// DialogConfirm ...
func DialogConfirm(title string, message string) bool {
	return dialogConfirmRunner(title, message, false)
}

// DialogConfirmFocused ...
func DialogConfirmFocused(title string, message string) bool {
	return dialogConfirmRunner(title, message, true)
}

func dialogConfirmRunner(title, message string, focused bool) bool {
	c1 := make(chan bool, 1)
	go func() {
		// Emulating left click to make "OK predefined"
		if focused {
			go func() {
				time.Sleep(time.Millisecond * 200)
				retVal := 0
				executeJSONRPC("Input.Left", &retVal, nil)
			}()
		}

		retVal := 0
		executeJSONRPCEx("Dialog_Confirm", &retVal, Args{title, message})
		c1 <- retVal != 0
	}()

	select {
	case res := <-c1:
		return res
	case <-time.After(time.Duration(DialogAutoclose) * time.Second):
		CloseAllDialogs()
		return focused
	}
}

// DialogText ...
func DialogText(title string, text string) bool {
	retVal := 0
	executeJSONRPCEx("Dialog_Text", &retVal, Args{title, text})
	return retVal != 0
}

// ListDialog ...
func ListDialog(title string, items ...string) int {
	retVal := -1
	executeJSONRPCEx("Dialog_Select", &retVal, Args{title, items})
	return retVal
}

// ListDialogLarge ...
func ListDialogLarge(title string, subject string, items ...string) int {
	retVal := -1
	executeJSONRPCEx("Dialog_Select_Large", &retVal, Args{title, subject, items})
	return retVal
}

// PlayerGetPlayingFile ...
func PlayerGetPlayingFile() string {
	retVal := ""
	executeJSONRPCEx("Player_GetPlayingFile", &retVal, nil)
	return retVal
}

// PlayerIsPlaying ...
func PlayerIsPlaying() bool {
	retVal := 0
	executeJSONRPCEx("Player_IsPlaying", &retVal, nil)
	return retVal != 0
}

// PlayerSeek ...
func PlayerSeek(position float64) (ret string) {
	if position <= 0 {
		return
	}

	executeJSONRPCEx("Player_Seek", &ret, Args{position})
	return
}

// PlayerIsPaused ...
func PlayerIsPaused() bool {
	retVal := 0
	executeJSONRPCEx("Player_IsPaused", &retVal, nil)
	return retVal != 0
}

// PlayerGetSubtitles ...
func PlayerGetSubtitles() (ret []string) {
	executeJSONRPCEx("Player_GetSubtitles", &ret, nil)
	return
}

// PlayerSetSubtitles ...
func PlayerSetSubtitles(urls []string) {
	executeJSONRPCEx("Player_SetSubtitles", nil, Args{urls})
}

// GetWatchTimes ...
func GetWatchTimes() map[string]string {
	var retVal map[string]string
	executeJSONRPCEx("Player_WatchTimes", &retVal, nil)
	return retVal
}

// CloseAllDialogs ...
func CloseAllDialogs() bool {
	retVal := 0
	executeJSONRPCEx("Dialog_CloseAll", &retVal, nil)
	return retVal != 0
}
