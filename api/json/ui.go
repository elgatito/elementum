package jsonapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/ui"
)

var badClientID = bad("client_id must be 1-64 letters, digits, - or _")

// GetUiEvents long-polls the UI broker. Texts are translated here, in the client's language.
func (Server) GetUiEvents(c *gin.Context, p GetUiEventsParams) {
	wait := 25
	if p.Wait != nil {
		wait = *p.Wait
	}
	lang, f := langArg(p.Lang)
	switch {
	case !ui.ClientIDRe.MatchString(p.ClientId):
		f = badClientID
	case wait < 0 || wait > 60 || (p.Since != nil && *p.Since < 0):
		f = bad("wait must be 0-60 and since 0 or more")
	}
	if f != nil {
		fail(c, f)
		return
	}
	events := ui.Default.Poll(c.Request.Context(), p.ClientId, ui.Default.SinceFor(derefOr(p.Epoch), p.Since), time.Duration(wait)*time.Second)
	epoch := ui.Default.Epoch()
	c.Header("X-UI-Epoch", epoch)
	if len(events) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	batch := UIEventBatch{Seq: events[len(events)-1].Seq, Epoch: epoch, Events: make([]UIEvent, len(events))}
	for i, e := range events {
		batch.Events[i] = uiEvent(e.Translated(lang))
	}
	c.JSON(http.StatusOK, batch)
}

func uiEvent(e ui.Event) UIEvent {
	out := UIEvent{
		Id: e.ID, Seq: e.Seq, Kind: UIEventKind(e.Kind), Title: e.Title, Message: e.Message,
		Default: e.Default,
	}
	if e.Options != nil {
		out.Options = &e.Options
	}
	if e.Preselect != nil {
		out.Preselect = &e.Preselect
	}
	if e.Multi {
		out.Multi = &e.Multi
	}
	if e.Hidden {
		out.Hidden = &e.Hidden
	}
	if e.TimeoutS > 0 {
		out.TimeoutS = &e.TimeoutS
	}
	if p := e.Progress; p != nil {
		out.Progress = &UIProgress{Id: p.ID, Percent: p.Percent, Heading: p.Heading, Line: p.Line, Done: p.Done}
	}
	return out
}

// AnswerUiRequest hands a client's answer to the broker.
func (Server) AnswerUiRequest(c *gin.Context, id string) {
	b := body[UIAnswer](c)
	if b == nil {
		return
	}
	if !ui.ClientIDRe.MatchString(b.ClientId) {
		fail(c, badClientID)
		return
	}
	a := ui.Answer{Selected: derefOr(b.Selected), Text: derefOr(b.Text), Yes: derefOr(b.Yes), Cancel: derefOr(b.Cancel)}
	switch err := ui.Default.Answer(id, a); {
	case errors.Is(err, ui.ErrUnknown):
		fail(c, failed(http.StatusNotFound, ErrorCodeNotFound, "unknown request"))
	case errors.Is(err, ui.ErrInvalid):
		fail(c, bad("invalid answer"))
	case errors.Is(err, ui.ErrClosed):
		fail(c, failed(http.StatusGone, ErrorCodeUiRequestExpired, "request is closed"))
	default:
		c.Status(http.StatusNoContent)
	}
}

func derefOr[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return
}
