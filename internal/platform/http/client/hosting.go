package client

import (
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
)

// hostingView is hosting as the settings show it (M9.1): the switch,
// whether the den serves and why not, and the ports Caddy and the router
// send it; and the den this install holds, once created, with when it
// was exported to move it to another computer, if it hasn't been hosted
// here since (M10).
type hostingView struct {
	// CanHost is false where the form hosts no den, as the Flatpak.
	CanHost      bool   `json:"can_host"`
	Enabled      bool   `json:"enabled"`
	Serving      bool   `json:"serving"`
	Problem      string `json:"problem,omitempty"`
	DenPort      int    `json:"den_port"`
	MediaUDPPort int    `json:"media_udp_port"`
	MediaTCPPort int    `json:"media_tcp_port"`
	Den          string `json:"den,omitempty"`
	URL          string `json:"url,omitempty"`
	// Exported is in Unix milliseconds.
	Exported int64 `json:"exported,omitempty"`
}

// cantHost is the page's word for a form that hosts no den (M9.3).
const cantHost = "Hosting a den needs Dens installed on Linux or Windows, where it keeps running when nobody is signed in."

func (rt *router) hostingView() (hostingView, error) {
	state := rt.a.Hosting.State()
	den := rt.a.Instance.Den
	v := hostingView{CanHost: rt.a.CanHost(), Enabled: state.On, Serving: state.Serving, Problem: state.Problem,
		DenPort: den.Port, MediaUDPPort: den.MediaUDPPort, MediaTCPPort: den.MediaTCPPort}
	if info, ok := rt.a.Den.Info(); ok {
		v.Den, v.URL = info.Name, info.URL
	}
	cfg, err := config.View(rt.a.DB)
	if err != nil {
		return v, err
	}
	if !cfg.DenExported.IsZero() {
		v.Exported = cfg.DenExported.UnixMilli()
	}
	return v, nil
}

func (rt *router) writeHosting(w http.ResponseWriter, r *http.Request) {
	v, err := rt.hostingView()
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, v)
}

func (rt *router) handleHosting(w http.ResponseWriter, r *http.Request) {
	rt.writeHosting(w, r)
}

// handleSetHosting turns hosting on after the local password, since it
// opens ports to the internet, or off, which the page has the member
// confirm.
func (rt *router) handleSetHosting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On       bool   `json:"on"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.On && !rt.a.CanHost() {
		jsonError(w, http.StatusConflict, cantHost)
		return
	}
	if body.On {
		if err := rt.a.VerifyPassword(r.Context(), body.Password); err != nil {
			passwordError(w, r, err)
			return
		}
	}
	var port *app.PortError
	switch err := rt.a.Hosting.Set(r.Context(), body.On); {
	case errors.As(err, &port):
		jsonError(w, http.StatusConflict, port.Error())
		return
	case errors.Is(err, app.ErrCantHost):
		jsonError(w, http.StatusConflict, cantHost)
		return
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.writeHosting(w, r)
}

// handleExportDen starts moving the den to another computer (M10): after
// the local password, it turns hosting off, so nothing reaches the den
// after the export's snapshot, and gives the page the link to download the
// export from, as a backup's.
func (rt *router) handleExportDen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if _, ok := rt.a.Den.Info(); !ok {
		jsonError(w, http.StatusConflict, "This computer holds no den to move.")
		return
	}
	if err := rt.a.VerifyPassword(r.Context(), body.Password); err != nil {
		passwordError(w, r, err)
		return
	}
	if rt.a.Hosting.State().On {
		if err := rt.a.Hosting.Set(r.Context(), false); err != nil {
			xhttp.Error(r.Context(), w, err)
			return
		}
	}
	token, err := rt.backups.issue(true)
	if err != nil {
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, map[string]string{"url": "/api/backup/" + token})
}

// handleRemoveDen deletes this computer's copy of the den once another
// computer serves it (M10), after the local password. A refusal says which
// of its checks didn't hold, and what to do.
func (rt *router) handleRemoveDen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var password error
	err := rt.a.RemoveDen(r.Context(), func() error {
		password = rt.a.VerifyPassword(r.Context(), body.Password)
		return password
	})
	var address *app.AddressError
	switch {
	case err == nil:
	case password != nil:
		passwordError(w, r, password)
		return
	case errors.Is(err, app.ErrHostingOn):
		jsonError(w, http.StatusConflict, "Hosting is on here, so this computer still serves the den. "+
			"Once another computer hosts it, turn hosting off here, then remove it.")
		return
	case errors.Is(err, app.ErrNotExported):
		jsonError(w, http.StatusConflict, "The den hasn't been exported since hosting was last on here, so no other computer holds all of it. "+
			"Move it to the other computer first.")
		return
	case errors.Is(err, app.ErrNoDen):
		jsonError(w, http.StatusConflict, "This computer holds no den.")
		return
	case errors.As(err, &address):
		jsonError(w, http.StatusBadGateway, address.Error())
		return
	default:
		xhttp.Error(r.Context(), w, err)
		return
	}
	rt.writeHosting(w, r)
}
