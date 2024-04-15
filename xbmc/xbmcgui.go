package xbmc

import (
	"time"
)

// DialogProgress ...
type DialogProgress struct {
	Host *XBMCHost
	hWnd string
}

// DialogProgressBG ...
type DialogProgressBG struct {
	Host *XBMCHost
	hWnd string
}

// OverlayStatus ...
type OverlayStatus struct {
	Host *XBMCHost
	hWnd string
}

// DialogInsert ...
func (h *XBMCHost) DialogInsert() map[string]string {
	var retVal map[string]string
	h.executeJSONRPCEx("DialogInsert", &retVal, nil)
	return retVal
}

// NewDialogProgress ...
func (h *XBMCHost) NewDialogProgress(title, line1, line2, line3 string) *DialogProgress {
	retVal := ""
	h.executeJSONRPCEx("DialogProgress_Create", &retVal, Args{title, line1, line2, line3})
	if len(retVal) <= 0 {
		return nil
	}
	return &DialogProgress{
		Host: h,
		hWnd: retVal,
	}
}

// Update ...
func (dp *DialogProgress) Update(percent int, line1, line2, line3 string) {
	retVal := -1
	dp.Host.executeJSONRPCEx("DialogProgress_Update", &retVal, Args{dp.hWnd, percent, dp.Host.TranslateText(line1), dp.Host.TranslateText(line2), dp.Host.TranslateText(line3)})
}

// IsCanceled ...
func (dp *DialogProgress) IsCanceled() bool {
	retVal := 0
	dp.Host.executeJSONRPCEx("DialogProgress_IsCanceled", &retVal, Args{dp.hWnd})
	return retVal != 0
}

// Close ...
func (dp *DialogProgress) Close() {
	retVal := -1
	dp.Host.executeJSONRPCEx("DialogProgress_Close", &retVal, Args{dp.hWnd})
}

// DialogProgressBGCleanup ...
func (h *XBMCHost) DialogProgressBGCleanup() {
	retVal := -1
	h.executeJSONRPCEx("DialogProgressBG_Cleanup", &retVal, Args{})
}

// NewDialogProgressBG ...
func (h *XBMCHost) NewDialogProgressBG(title, message string, translations ...string) *DialogProgressBG {
	retVal := ""
	h.executeJSONRPCEx("DialogProgressBG_Create", &retVal, Args{title, message, translations})
	if len(retVal) <= 0 {
		return nil
	}
	return &DialogProgressBG{
		Host: h,
		hWnd: retVal,
	}
}

// Update ...
func (dp *DialogProgressBG) Update(percent int, heading string, message string) {
	retVal := -1
	dp.Host.executeJSONRPCEx("DialogProgressBG_Update", &retVal, Args{dp.hWnd, percent, dp.Host.TranslateText(heading), dp.Host.TranslateText(message)})
}

// IsFinished ...
func (dp *DialogProgressBG) IsFinished() bool {
	retVal := 0
	dp.Host.executeJSONRPCEx("DialogProgressBG_IsFinished", &retVal, Args{dp.hWnd})
	return retVal != 0
}

// Close ...
func (dp *DialogProgressBG) Close() {
	retVal := -1
	dp.Host.executeJSONRPCEx("DialogProgressBG_Close", &retVal, Args{dp.hWnd})
}

// NewOverlayStatus ...
func (h *XBMCHost) NewOverlayStatus() *OverlayStatus {
	retVal := ""
	h.executeJSONRPCEx("OverlayStatus_Create", &retVal, Args{})
	if len(retVal) <= 0 {
		return nil
	}
	return &OverlayStatus{
		Host: h,
		hWnd: retVal,
	}
}

// Update ...
func (ov *OverlayStatus) Update(percent int, line1, line2, line3 string) {
	if ov == nil {
		return
	}

	retVal := -1
	ov.Host.executeJSONRPCEx("OverlayStatus_Update", &retVal, Args{ov.hWnd, percent, ov.Host.TranslateText(line1), ov.Host.TranslateText(line2), ov.Host.TranslateText(line3)})
}

// Show ...
func (ov *OverlayStatus) Show() {
	if ov == nil {
		return
	}

	retVal := -1
	ov.Host.executeJSONRPCEx("OverlayStatus_Show", &retVal, Args{ov.hWnd})
}

// Hide ...
func (ov *OverlayStatus) Hide() {
	if ov == nil {
		return
	}

	retVal := -1
	ov.Host.executeJSONRPCEx("OverlayStatus_Hide", &retVal, Args{ov.hWnd})
}

// Close ...
func (ov *OverlayStatus) Close() {
	if ov == nil {
		return
	}

	retVal := -1
	ov.Host.executeJSONRPCEx("OverlayStatus_Close", &retVal, Args{ov.hWnd})
}

// Notify ...
func (h *XBMCHost) Notify(header string, message string, image string) {
	var retVal string
	h.executeJSONRPCEx("Notify", &retVal, Args{header, message, image})
}

// InfoLabels ...
func (h *XBMCHost) InfoLabels(labels ...string) map[string]string {
	var retVal map[string]string
	h.executeJSONRPC("XBMC.GetInfoLabels", &retVal, Args{labels})
	return retVal
}

// InfoLabel ...
func (h *XBMCHost) InfoLabel(label string) string {
	labels := h.InfoLabels(label)
	return labels[label]
}

// GetWindowProperty ...
func (h *XBMCHost) GetWindowProperty(key string) string {
	var retVal string
	h.executeJSONRPCEx("GetWindowProperty", &retVal, Args{key})
	return retVal
}

// SetWindowProperty ...
func (h *XBMCHost) SetWindowProperty(key string, value string) {
	var retVal string
	h.executeJSONRPCEx("SetWindowProperty", &retVal, Args{key, value})
}

// Keyboard ...
func (h *XBMCHost) Keyboard(args ...interface{}) string {
	var retVal string
	h.executeJSONRPCEx("Keyboard", &retVal, args)
	return retVal
}

// Dialog ...
func (h *XBMCHost) Dialog(title string, message string) bool {
	retVal := 0
	h.executeJSONRPCEx("Dialog", &retVal, Args{title, message})
	return retVal != 0
}

// DialogBrowseSingle ...
func (h *XBMCHost) DialogBrowseSingle(browseType int, title string, shares string, mask string, useThumbs bool, treatAsFolder bool, defaultt string) string {
	retVal := ""
	h.executeJSONRPCEx("Dialog_Browse_Single", &retVal, Args{browseType, title, shares, mask, useThumbs, treatAsFolder, defaultt})
	return retVal
}

// DialogConfirmNonTimed is a regular Yes/No dialog, without time limitation applied.
func (h *XBMCHost) DialogConfirmNonTimed(title string, message string) bool {
	retVal := 0
	h.executeJSONRPCEx("Dialog_Confirm", &retVal, Args{title, message})
	return retVal != 0
}

// DialogConfirm ...
func (h *XBMCHost) DialogConfirm(title string, message string) bool {
	return h.dialogConfirmRunner(title, message, false)
}

// DialogConfirmFocused ...
func (h *XBMCHost) DialogConfirmFocused(title string, message string) bool {
	return h.dialogConfirmRunner(title, message, true)
}

func (h *XBMCHost) dialogConfirmRunner(title, message string, focused bool) bool {
	c1 := make(chan bool, 1)
	go func() {
		// Emulating left click to make "OK predefined"
		if focused {
			go func() {
				time.Sleep(time.Millisecond * 200)
				retVal := 0
				h.executeJSONRPC("Input.Left", &retVal, nil)
			}()
		}

		retVal := 0
		h.executeJSONRPCEx("Dialog_Confirm_With_Timeout", &retVal, Args{title, message, focused, DialogAutoclose})
		c1 <- retVal != 0
	}()

	select {
	case res := <-c1:
		return res
	case <-time.After(time.Duration(DialogAutoclose) * time.Second):
		h.CloseAllConfirmDialogs()
		return focused
	}
}

// DialogText ...
func (h *XBMCHost) DialogText(title string, text string) bool {
	retVal := 0
	h.executeJSONRPCEx("Dialog_Text", &retVal, Args{title, text})
	return retVal != 0
}

// ListDialog ...
func (h *XBMCHost) ListDialog(title string, items ...string) int {
	retVal := -1
	h.executeJSONRPCEx("Dialog_Select", &retVal, Args{title, items})
	return retVal
}

// ListDialogWithOptions adds autoclose and preselect to a dialog
func (h *XBMCHost) ListDialogWithOptions(autoclose, preselect int, title string, items ...string) int {
	retVal := -1
	h.executeJSONRPCEx("Dialog_Select_With_Options", &retVal, Args{title, items, autoclose, preselect})
	return retVal
}

// ListDialogLarge ...
func (h *XBMCHost) ListDialogLarge(title string, subject string, items ...string) int {
	retVal := -1
	h.executeJSONRPCEx("Dialog_Select_Large", &retVal, Args{title, subject, items})
	return retVal
}

// PlayerGetPlayingFile ...
func (h *XBMCHost) PlayerGetPlayingFile() string {
	retVal := ""
	h.executeJSONRPCEx("Player_GetPlayingFile", &retVal, nil)
	return retVal
}

// PlayerIsPlaying ...
func (h *XBMCHost) PlayerIsPlaying() bool {
	retVal := 0
	h.executeJSONRPCEx("Player_IsPlaying", &retVal, nil)
	return retVal != 0
}

// PlayerSeek ...
func (h *XBMCHost) PlayerSeek(position float64) (ret string) {
	if position <= 0 {
		return
	}

	h.executeJSONRPCEx("Player_Seek", &ret, Args{position})
	return
}

// PlayerIsPaused ...
func (h *XBMCHost) PlayerIsPaused() bool {
	retVal := 0
	h.executeJSONRPCEx("Player_IsPaused", &retVal, nil)
	return retVal != 0
}

// PlayerGetSubtitles ...
func (h *XBMCHost) PlayerGetSubtitles() (ret []string) {
	h.executeJSONRPCEx("Player_GetSubtitles", &ret, nil)
	return
}

// PlayerSetSubtitles ...
func (h *XBMCHost) PlayerSetSubtitles(urls []string) {
	h.executeJSONRPCEx("Player_SetSubtitles", nil, Args{urls})
}

// GetWatchTimes ...
func (h *XBMCHost) GetWatchTimes() map[string]string {
	var retVal map[string]string
	h.executeJSONRPCEx("Player_WatchTimes", &retVal, nil)
	return retVal
}

// CloseAllDialogs ...
func (h *XBMCHost) CloseAllDialogs() bool {
	retVal := 0
	h.executeJSONRPCEx("Dialog_CloseAll", &retVal, nil)
	return retVal != 0
}

// CloseAllConfirmDialogs ...
func (h *XBMCHost) CloseAllConfirmDialogs() bool {
	retVal := 0
	h.executeJSONRPCEx("Dialog_CloseAllConfirms", &retVal, nil)
	return retVal != 0
}
