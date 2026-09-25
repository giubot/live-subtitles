// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

// Recordings is the recordings service (internal/recording.Recorder).
type Recordings interface {
	List(ctx context.Context, sessionID string) ([]api.Recording, error)
	Get(ctx context.Context, id string) (api.Recording, error)
	Delete(ctx context.Context, id string) error
	// Open returns the audio file, or domain.ErrNotFound.
	Open(ctx context.Context, id string) (*os.File, api.Recording, error)
	Usage(ctx context.Context) (api.StorageUsage, error)
}

var recordingNotFound = api.NotFoundJSONResponse{Code: "recording.not_found", Message: "recording not found"}

func (s *Server) ListRecordings(ctx context.Context, req api.ListRecordingsRequestObject) (api.ListRecordingsResponseObject, error) {
	if s.Recordings == nil {
		return nil, api.ErrNotImplemented
	}
	var sessionID string
	if req.Params.SessionId != nil {
		sessionID = *req.Params.SessionId
	}
	list, err := s.Recordings.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return api.ListRecordings200JSONResponse(list), nil
}

func (s *Server) GetRecordingUsage(ctx context.Context, _ api.GetRecordingUsageRequestObject) (api.GetRecordingUsageResponseObject, error) {
	if s.Recordings == nil {
		return nil, api.ErrNotImplemented
	}
	u, err := s.Recordings.Usage(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetRecordingUsage200JSONResponse(u), nil
}

func (s *Server) GetRecording(ctx context.Context, req api.GetRecordingRequestObject) (api.GetRecordingResponseObject, error) {
	if s.Recordings == nil {
		return nil, api.ErrNotImplemented
	}
	rec, err := s.Recordings.Get(ctx, req.RecordingId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.GetRecording404JSONResponse{NotFoundJSONResponse: recordingNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetRecording200JSONResponse(rec), nil
}

func (s *Server) DeleteRecording(ctx context.Context, req api.DeleteRecordingRequestObject) (api.DeleteRecordingResponseObject, error) {
	if s.Recordings == nil {
		return nil, api.ErrNotImplemented
	}
	err := s.Recordings.Delete(ctx, req.RecordingId)
	if errors.Is(err, domain.ErrNotFound) {
		return api.DeleteRecording404JSONResponse{NotFoundJSONResponse: recordingNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.DeleteRecording204Response{}, nil
}

// serveRecordingAudio serves getRecordingAudio outside the strict handler:
// http.ServeContent answers Range requests (206), which the <audio>
// element uses to seek (REC-2). A recording still being written grows, so
// it isn't cached.
func (s *Server) serveRecordingAudio(w http.ResponseWriter, r *http.Request, id string) {
	f, rec, err := s.Recordings.Open(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		WriteError(w, http.StatusNotFound, recordingNotFound.Code, recordingNotFound.Message)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "open recording", "recording", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		s.log.ErrorContext(r.Context(), "stat recording", "recording", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	w.Header().Set("Content-Type", "audio/mp4")
	if rec.Status == api.RecordingStatusRecording {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// recordingWindow is the part of the session clock a recording covers,
// for captions requested with recordingId: [from, to) where from is its
// offset, open-ended while it records. ok is false when the recording
// doesn't exist or belongs to another session.
func (s *Server) recordingWindow(ctx context.Context, sessionID, recordingID string) (from, to time.Duration, ok bool, err error) {
	rec, err := s.Recordings.Get(ctx, recordingID)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && rec.SessionId != sessionID) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if rec.OffsetSec != nil {
		from = seconds(*rec.OffsetSec)
	}
	to = time.Duration(math.MaxInt64)
	if rec.Status != api.RecordingStatusRecording && rec.DurationSec != nil {
		to = from + seconds(*rec.DurationSec)
	}
	return from, to, true, nil
}

func seconds(s float32) time.Duration { return time.Duration(float64(s) * float64(time.Second)) }

// shift moves captions from the session clock onto a recording's clock.
func shift(items []api.Caption, offset time.Duration) []api.Caption {
	if offset == 0 {
		return items
	}
	off := float32(offset.Seconds())
	for i := range items {
		items[i].Start = max(0, items[i].Start-off)
		items[i].End = max(0, items[i].End-off)
	}
	return items
}
