// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"context"
	"net/http"

	"github.com/iencodev/live-subtitles/internal/api"
)

// TLSService describes the HTTPS listener (tlsutil.Manager).
type TLSService interface {
	Info() api.TlsInfo
	// CACertPEM is the local CA certificate; nil when the mode isn't local-ca.
	CACertPEM() []byte
}

// CACertFilename is the name browsers save the CA download as.
const CACertFilename = "livesubs-ca.crt"

func (s *Server) GetTlsInfo(context.Context, api.GetTlsInfoRequestObject) (api.GetTlsInfoResponseObject, error) {
	if s.TLS == nil {
		return nil, api.ErrNotImplemented
	}
	return api.GetTlsInfo200JSONResponse(s.TLS.Info()), nil
}

func (s *Server) DownloadCaCert(context.Context, api.DownloadCaCertRequestObject) (api.DownloadCaCertResponseObject, error) {
	if s.TLS == nil {
		return nil, api.ErrNotImplemented
	}
	pem := s.TLS.CACertPEM()
	if pem == nil {
		return api.DownloadCaCert404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse{
			Code:    "tls.no_local_ca",
			Message: "there is no local CA: HTTPS uses a provided or Let's Encrypt certificate, or is disabled",
		}}, nil
	}
	return caCertDownload{api.DownloadCaCert200ApplicationxX509CaCertResponse{
		Body:          bytes.NewReader(pem),
		ContentLength: int64(len(pem)),
	}}, nil
}

// caCertDownload adds the attachment filename to the generated response,
// so phones and desktops offer to save or install the file.
type caCertDownload struct {
	api.DownloadCaCert200ApplicationxX509CaCertResponse
}

func (r caCertDownload) VisitDownloadCaCertResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Disposition", `attachment; filename="`+CACertFilename+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	return r.DownloadCaCert200ApplicationxX509CaCertResponse.VisitDownloadCaCertResponse(w)
}
