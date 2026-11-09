package apiv1

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/ui"
)

// GetUiEvents long-polls the UI broker. Texts are translated here, in the client's language.
func (Server) GetUiEvents(ctx context.Context, req GetUiEventsRequestObject) (GetUiEventsResponseObject, error) {
	p := req.Params
	if !ui.ClientIDRe.MatchString(p.ClientId) {
		return GetUiEvents400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "client_id must be 1-64 letters, digits, - or _"))}, nil
	}
	wait := 25
	if p.Wait != nil {
		wait = *p.Wait
	}
	if wait < 0 || wait > 60 || (p.Since != nil && *p.Since < 0) {
		return GetUiEvents400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "wait must be 0-60 and since 0 or more"))}, nil
	}
	lang := config.Get().Language
	if p.Lang != nil {
		m := langRe.FindStringSubmatch(*p.Lang)
		if m == nil {
			return GetUiEvents400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "lang must look like de or de-DE"))}, nil
		}
		lang = strings.ToLower(m[1])
		if m[2] != "" {
			lang += "-" + strings.ToUpper(m[2])
		}
	}
	// The strict server's ctx is the *gin.Context, which is not cancelled with the request.
	reqCtx := ctx.(*gin.Context).Request.Context()
	events := ui.Default.Poll(reqCtx, p.ClientId, ui.Default.SinceFor(derefOr(p.Epoch), p.Since), time.Duration(wait)*time.Second)
	epoch := ui.Default.Epoch()
	if len(events) == 0 {
		return GetUiEvents204Response{Headers: GetUiEvents204ResponseHeaders{XUIEpoch: &epoch}}, nil
	}
	batch := UIEventBatch{Seq: events[len(events)-1].Seq, Epoch: epoch, Events: make([]UIEvent, len(events))}
	for i, e := range events {
		batch.Events[i] = uiEvent(e.Translated(lang))
	}
	return GetUiEvents200JSONResponse{Body: batch, Headers: GetUiEvents200ResponseHeaders{XUIEpoch: &epoch}}, nil
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
func (Server) AnswerUiRequest(_ context.Context, req AnswerUiRequestRequestObject) (AnswerUiRequestResponseObject, error) {
	b := req.Body
	if b == nil || !ui.ClientIDRe.MatchString(b.ClientId) {
		return AnswerUiRequest400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "client_id must be 1-64 letters, digits, - or _"))}, nil
	}
	a := ui.Answer{Selected: derefOr(b.Selected), Text: derefOr(b.Text), Yes: derefOr(b.Yes), Cancel: derefOr(b.Cancel)}
	switch err := ui.Default.Answer(req.Id, a); {
	case errors.Is(err, ui.ErrUnknown):
		return AnswerUiRequest404JSONResponse{NotFoundJSONResponse(newError(ErrorCodeNotFound, "unknown request"))}, nil
	case errors.Is(err, ui.ErrInvalid):
		return AnswerUiRequest400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "invalid answer"))}, nil
	case errors.Is(err, ui.ErrClosed):
		return AnswerUiRequest410JSONResponse(newError(ErrorCodeUiRequestExpired, "request is closed")), nil
	}
	return AnswerUiRequest204Response{}, nil
}

func derefOr[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return
}
