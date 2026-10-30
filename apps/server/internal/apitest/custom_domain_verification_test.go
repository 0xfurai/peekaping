package apitest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// PP-165: with VERIFY_CUSTOM_DOMAINS on, a custom domain serves its page and gets a certificate
// only once a TXT record or a CNAME proves it points at the install.

// withVerifiedCustomDomains turns verification on, with the CNAME target the test needs
func withVerifiedCustomDomains(cnameTarget string) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.VerifyCustomDomains = true
		cfg.CustomDomainCNAMETarget = cnameTarget
	}
}

// withVerifyCustomDomains sets VERIFY_CUSTOM_DOMAINS to the value a test needs, whatever the run's
// default is
func withVerifyCustomDomains(on bool) func(*config.Config) {
	return func(cfg *config.Config) { cfg.VerifyCustomDomains = on }
}

// dnsRecords is the resolver of a test: the records of a name it was given, and a lookup error for
// any other
type dnsRecords struct {
	mu    sync.Mutex
	txt   map[string][]string
	cname map[string]string
}

func (d *dnsRecords) LookupTXT(_ context.Context, name string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if values, ok := d.txt[name]; ok {
		return values, nil
	}
	return nil, errors.New("no such host")
}

func (d *dnsRecords) LookupCNAME(_ context.Context, host string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if target, ok := d.cname[host]; ok {
		return target, nil
	}
	return "", errors.New("no such host")
}

func (d *dnsRecords) setTXT(name string, values ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.txt[name] = values
}

func (d *dnsRecords) setCNAME(host, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cname[host] = target
}

// newAppWithDNS is an app whose resolver is the returned one
func newAppWithDNS(t *testing.T, options ...func(*config.Config)) (*app, *dnsRecords) {
	t.Helper()
	dns := &dnsRecords{txt: map[string][]string{}, cname: map[string]string{}}
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(domain_status_page.Resolver) domain_status_page.Resolver { return dns })
	}, options...)
	a.registerAdmin(t)
	return a, dns
}

type domainRecordsResponse struct {
	VerificationRequired bool `json:"verificationRequired"`
	Domains              []struct {
		Domain        string     `json:"domain"`
		State         string     `json:"state"`
		VerifiedAt    *time.Time `json:"verifiedAt"`
		LastCheckedAt *time.Time `json:"lastCheckedAt"`
		FailingSince  *time.Time `json:"failingSince"`
		RemoveAt      *time.Time `json:"removeAt"`
		Records       struct {
			TXT   struct{ Name, Value string }   `json:"txt"`
			CNAME *struct{ Name, Target string } `json:"cname"`
		} `json:"records"`
	} `json:"domains"`
}

func domainsPath(page string) string { return statusPagesPath + "/domains/" + page }

func domainVerifyPath(page, domain string) string {
	return domainsPath(page) + "/" + url.PathEscape(domain) + "/verify"
}

func tlsAsk(domain string) string {
	return "/api/v1/status-pages/tls-ask?domain=" + url.QueryEscape(domain)
}

func domainPath(domain string) string { return "/api/v1/status-pages/domain/" + domain }

// servedStatuses are the answers of the two routes that act on a domain's being verified
func servedStatuses(t *testing.T, a *app, domain string) (page, ask int) {
	t.Helper()
	return a.request(t, http.MethodGet, domainPath(domain), nil).Code, a.request(t, http.MethodGet, tlsAsk(domain), nil).Code
}

// AC1: with verification off, which is the default, a domain serves as soon as it is added
func TestCustomDomainVerification_OffServesEveryDomainAsBefore(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifyCustomDomains(false))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})

	page, ask := servedStatuses(t, a, "status.shop.example")
	assert.Equal(t, http.StatusOK, page)
	assert.Equal(t, http.StatusOK, ask)

	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)
	assert.False(t, list.VerificationRequired)
	require.Len(t, list.Domains, 1)
	assert.Nil(t, list.Domains[0].RemoveAt, "nothing is removed while verification is off")
}

// AC2: with verification on, a new domain is Pending, and neither serves nor gets a certificate
func TestCustomDomainVerification_ANewDomainIsNotServedUntilVerified(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})

	page, ask := servedStatuses(t, a, "status.shop.example")
	assert.Equal(t, http.StatusNotFound, page)
	assert.Equal(t, http.StatusNotFound, ask)

	w := a.request(t, http.MethodGet, domainPath("status.shop.example"), nil)
	assert.NotContains(t, w.Body.String(), "shop", "it answers as an unknown domain does")
	unknown := a.request(t, http.MethodGet, domainPath("unknown.example"), nil)
	assert.Equal(t, unknown.Body.String(), w.Body.String())
}

// AC3
func TestCustomDomainVerification_ATXTRecordVerifiesTheDomain(t *testing.T) {
	a, dns := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"Status.Shop.Example"}})

	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)
	require.Len(t, list.Domains, 1)
	record := list.Domains[0].Records.TXT
	assert.Equal(t, "_peekaping-verify.status.shop.example", record.Name)
	assert.Regexp(t, "^peekaping-verify=[0-9a-f]{64}$", record.Value)

	// The check finds nothing yet
	w := a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin)
	assert.Equal(t, "pending", decode[domainStatusResponse](t, w, http.StatusOK).State)
	page, ask := servedStatuses(t, a, "status.shop.example")
	assert.Equal(t, http.StatusNotFound, page)
	assert.Equal(t, http.StatusNotFound, ask)

	dns.setTXT(record.Name, record.Value)
	w = a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin)
	got := decode[domainStatusResponse](t, w, http.StatusOK)
	assert.Equal(t, "verified", got.State)
	assert.NotNil(t, got.VerifiedAt)
	assert.NotNil(t, got.LastCheckedAt)
	page, ask = servedStatuses(t, a, "status.shop.example")
	assert.Equal(t, http.StatusOK, page)
	assert.Equal(t, http.StatusOK, ask)
}

type domainStatusResponse = struct {
	Domain        string     `json:"domain"`
	State         string     `json:"state"`
	VerifiedAt    *time.Time `json:"verifiedAt"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
	FailingSince  *time.Time `json:"failingSince"`
	RemoveAt      *time.Time `json:"removeAt"`
}

// AC3
func TestCustomDomainVerification_ACNAMEToTheTargetVerifiesTheDomain(t *testing.T) {
	a, dns := newAppWithDNS(t, withVerifiedCustomDomains("Peek.Example"))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})
	dns.setCNAME("status.shop.example", "peek.example.")

	got := decode[domainStatusResponse](t, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "verified", got.State)
	page, ask := servedStatuses(t, a, "status.shop.example")
	assert.Equal(t, http.StatusOK, page)
	assert.Equal(t, http.StatusOK, ask)
}

// AC3: without a configured target, the host of the public URL is the target
func TestCustomDomainVerification_TheTargetDefaultsToThePublicURLsHost(t *testing.T) {
	a, dns := newAppWithDNS(t, withVerifyCustomDomains(true))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})

	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)
	require.NotNil(t, list.Domains[0].Records.CNAME)
	assert.Equal(t, "status.shop.example", list.Domains[0].Records.CNAME.Name)
	assert.Equal(t, "peek.example", list.Domains[0].Records.CNAME.Target, "the host of "+clientURL)

	dns.setCNAME("status.shop.example", "peek.example")
	got := decode[domainStatusResponse](t, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "verified", got.State)
}

// AC5: the workspace sees the state and the records to add, and no other workspace's page
func TestCustomDomainVerification_ListShowsTheStateAndTheRecordsToAdd(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	id := a.createStatusPage(t, statusPage{slug: "shop", domains: []string{"b.shop.example", "a.shop.example"}})

	w := a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin)
	list := decode[domainRecordsResponse](t, w, http.StatusOK)
	assert.True(t, list.VerificationRequired)
	require.Len(t, list.Domains, 2)
	tokens := map[string]bool{}
	for _, d := range list.Domains {
		assert.Equal(t, "pending", d.State)
		assert.Nil(t, d.VerifiedAt)
		assert.Nil(t, d.LastCheckedAt, "never checked")
		assert.Nil(t, d.RemoveAt, "its week starts at the first check")
		assert.Equal(t, "_peekaping-verify."+d.Domain, d.Records.TXT.Name)
		require.NotNil(t, d.Records.CNAME)
		assert.Equal(t, "peek.example", d.Records.CNAME.Target)
		tokens[d.Records.TXT.Value] = true
	}
	assert.Len(t, tokens, 2, "each domain has a token of its own")

	again := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, list, again, "the token stays the same")
}

// The token is the secret that proves a domain, so no public route shows it
func TestCustomDomainVerification_NoPublicRouteShowsTheToken(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifyCustomDomains(false))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})
	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)
	token := list.Domains[0].Records.TXT.Value[len("peekaping-verify="):]

	for _, path := range []string{
		"/api/v1/status-pages/slug/shop", domainPath("status.shop.example"), tlsAsk("status.shop.example"),
		"/api/v1/status-pages/slug/shop/monitors", statusPagesPath + "/" + id, statusPagesPath,
	} {
		w := a.request(t, http.MethodGet, path, nil, a.asAdmin)
		assert.NotContains(t, w.Body.String(), token, path)
		assert.NotContains(t, w.Body.String(), "verification", path)
	}
}

func TestCustomDomainVerification_AnotherWorkspacesPageIsNotFoundAndASignedOutCallerIsRefused(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	_, bob := bobsWorkspace(t, a)
	bobs := a.createStatusPageAs(t, bob, statusPage{slug: "bobs", published: true, domains: []string{"bob.example"}})

	for name, c := range map[string]call{
		"list":   {method: http.MethodGet, path: domainsPath(bobs)},
		"verify": {method: http.MethodPost, path: domainVerifyPath(bobs, "bob.example")},
		"gone":   {method: http.MethodGet, path: domainsPath(missingRecordID)},
	} {
		t.Run(name+" in another workspace", func(t *testing.T) {
			w := a.request(t, c.method, c.path, nil, a.asAdmin)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), "bob.example")
		})
		t.Run(name+" signed out", func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, a.request(t, c.method, c.path, nil).Code)
		})
	}
	// Bob sees his own
	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(bobs), nil, bob), http.StatusOK)
	assert.Len(t, list.Domains, 1)
}

func TestCustomDomainVerification_VerifyOfADomainThePageDoesNotListIsNotFound(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	mine := a.createStatusPage(t, statusPage{slug: "mine", domains: []string{"mine.example"}})
	a.createStatusPage(t, statusPage{slug: "other", domains: []string{"other.example"}})

	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPost, domainVerifyPath(mine, "other.example"), nil, a.asAdmin).Code, "a domain of another page")
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPost, domainVerifyPath(mine, "unknown.example"), nil, a.asAdmin).Code)
}

// A check can be asked for with verification off, to see the records work before turning it on
func TestCustomDomainVerification_VerifyWorksWhileVerificationIsOff(t *testing.T) {
	a, dns := newAppWithDNS(t, withVerifyCustomDomains(false))
	id := a.createStatusPage(t, statusPage{slug: "shop", published: true, domains: []string{"status.shop.example"}})
	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, a.asAdmin), http.StatusOK)

	got := decode[domainStatusResponse](t, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "pending", got.State)
	assert.NotNil(t, got.LastCheckedAt)
	assert.Nil(t, got.RemoveAt, "and starts no clock")

	dns.setTXT("_peekaping-verify.status.shop.example", list.Domains[0].Records.TXT.Value)
	got = decode[domainStatusResponse](t, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "verified", got.State)
}

// A member of the workspace manages its status pages and custom domains (docs/adr/0005 section 1),
// and so sees the records to add and asks for a check
func TestCustomDomainVerification_AMemberOfTheWorkspaceSeesAndChecksItsDomains(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	id := a.createStatusPage(t, statusPage{slug: "shop", domains: []string{"status.shop.example"}})
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)

	list := decode[domainRecordsResponse](t, a.request(t, http.MethodGet, domainsPath(id), nil, member), http.StatusOK)
	assert.Len(t, list.Domains, 1)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, member).Code)
}

// AC8: the API says when a Pending domain will be removed
func TestCustomDomainVerification_ACheckedPendingDomainSaysWhenItWillBeRemoved(t *testing.T) {
	a, _ := newAppWithDNS(t, withVerifiedCustomDomains("peek.example"))
	id := a.createStatusPage(t, statusPage{slug: "shop", domains: []string{"status.shop.example"}})

	before := time.Now()
	got := decode[domainStatusResponse](t, a.request(t, http.MethodPost, domainVerifyPath(id, "status.shop.example"), nil, a.asAdmin), http.StatusOK)
	require.NotNil(t, got.RemoveAt)
	assert.WithinDuration(t, before.Add(7*24*time.Hour), *got.RemoveAt, time.Minute)
}

func TestCustomDomainVerification_MeStatesTheSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		options []func(*config.Config)
		verify  bool
		target  string
	}{
		"off":                   {options: []func(*config.Config){withVerifyCustomDomains(false)}, verify: false, target: "peek.example"},
		"on, target configured": {options: []func(*config.Config){withVerifiedCustomDomains("Status.Example.COM.")}, verify: true, target: "status.example.com"},
		"on, public URL's host": {options: []func(*config.Config){withVerifyCustomDomains(true)}, verify: true, target: "peek.example"},
		"on, localhost":         {options: []func(*config.Config){withVerifyCustomDomains(true), func(c *config.Config) { c.ClientURL = "http://localhost:8383" }}, verify: true, target: ""},
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := newAppWithDNS(t, tc.options...)
			me := decode[struct {
				VerifyCustomDomains     bool   `json:"verifyCustomDomains"`
				CustomDomainCNAMETarget string `json:"customDomainCnameTarget"`
			}](t, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin), http.StatusOK)
			assert.Equal(t, tc.verify, me.VerifyCustomDomains)
			assert.Equal(t, tc.target, me.CustomDomainCNAMETarget)
		})
	}
}
