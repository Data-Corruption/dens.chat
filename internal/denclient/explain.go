package denclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// Explain describes a failure to reach a den for the member, naming the
// usual misconfigurations instead of showing Go's error text.
func Explain(err error) string {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var dns *net.DNSError
	var netErr net.Error
	switch {
	case err == nil:
		return ""
	// net/http reports this with a plain error, so only its text identifies it.
	case strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"):
		return "The den's address starts with https://, but the server there answers plain HTTP. " +
			"A den on the internet needs Caddy in front of it; one on this computer uses an http:// address with its den port."
	case errors.As(err, &unknown):
		return "The den's certificate isn't from an authority this computer trusts."
	case errors.As(err, &hostname):
		return "The den's certificate is for a different name than its address."
	case errors.As(err, &invalid):
		return "The den's certificate has expired or isn't valid yet."
	case errors.As(err, &verify):
		return "The den's certificate can't be verified."
	case errors.As(err, &dns):
		return "Can't find the den's address: the name " + dns.Name + " doesn't resolve."
	case host.ConnRefused(err):
		return "Nothing is answering at the den's address. The den or its Caddy may be down."
	case errors.As(err, &netErr) && netErr.Timeout():
		return "The den didn't answer in time."
	}
	return "Can't reach the den. Check its address and your connection."
}

// ExplainStatus describes an answer at a den's address that isn't the
// protocol's: a proxy with no den behind it, or a server that isn't a den.
func ExplainStatus(e *denproto.Error) (string, bool) {
	if !strings.HasPrefix(e.Code, "http_") {
		return "", false
	}
	switch e.Status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Sprintf("Something answers at the den's address, but the den behind it doesn't (HTTP %d). It may be down or restarting.", e.Status), true
	}
	return fmt.Sprintf("The server at that address doesn't answer like a den (HTTP %d). Check the address.", e.Status), true
}
