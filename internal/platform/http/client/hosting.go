package client

import (
	"errors"
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
)

// hostingView is hosting as the settings show it (M9.1): the switch,
// whether the den serves and why not, and the ports Caddy and the router
// send it.
type hostingView struct {
	Enabled      bool   `json:"enabled"`
	Serving      bool   `json:"serving"`
	Problem      string `json:"problem,omitempty"`
	DenPort      int    `json:"den_port"`
	MediaUDPPort int    `json:"media_udp_port"`
	MediaTCPPort int    `json:"media_tcp_port"`
}

func (rt *router) hostingView() hostingView {
	state := rt.a.Hosting.State()
	den := rt.a.Instance.Den
	return hostingView{Enabled: state.On, Serving: state.Serving, Problem: state.Problem,
		DenPort: den.Port, MediaUDPPort: den.MediaUDPPort, MediaTCPPort: den.MediaTCPPort}
}

func (rt *router) handleHosting(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, rt.hostingView())
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
	case err != nil:
		xhttp.Error(r.Context(), w, err)
		return
	}
	writeJSON(w, rt.hostingView())
}
