package executor

import (
	"context"
	"fmt"
	"net"
	"peekaping/internal/modules/shared"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"
)

type DNSConfig struct {
	Host           string `json:"host" validate:"required" example:"example.com"`
	ResolverServer string `json:"resolver_server" validate:"required,ip" example:"1.1.1.1"`
	Port           int    `json:"port" validate:"required,min=1,max=65535" example:"53"`
	ResolveType    string `json:"resolve_type" validate:"required,oneof=A AAAA CAA CNAME MX NS PTR SOA SRV TXT" example:"A"`
}

type DNSExecutor struct {
	guarded
	logger *zap.SugaredLogger
}

func NewDNSExecutor(logger *zap.SugaredLogger) *DNSExecutor {
	return &DNSExecutor{
		logger: logger,
	}
}

func (s *DNSExecutor) Unmarshal(configJSON string) (any, error) {
	return GenericUnmarshal[DNSConfig](configJSON)
}

func (s *DNSExecutor) Validate(configJSON string) error {
	cfg, err := s.Unmarshal(configJSON)
	if err != nil {
		return err
	}
	return GenericValidator(cfg.(*DNSConfig))
}

// Targets lists the resolver a DNS check queries: the name being looked up is the resolver's to
// resolve, and not something the check connects to
func (s *DNSExecutor) Targets(configJSON string) ([]string, error) {
	cfg, err := GenericUnmarshal[DNSConfig](configJSON)
	if err != nil {
		return nil, err
	}
	return []string{cfg.ResolverServer}, nil
}

// dnsQuery is what a record lookup needs besides the context: the resolver to ask, the check's
// config and the monitor's timeout.
type dnsQuery struct {
	resolver *net.Resolver
	cfg      *DNSConfig
	timeout  time.Duration
}

// dnsLookup looks up one record type and describes what it found. An empty message means the
// lookup succeeded but found no records.
type dnsLookup func(d *DNSExecutor, ctx context.Context, q dnsQuery) (string, error)

// dnsLookups maps an upper-cased resolve type to its lookup
var dnsLookups = map[string]dnsLookup{
	"A":     (*DNSExecutor).lookupA,
	"AAAA":  (*DNSExecutor).lookupAAAA,
	"CNAME": (*DNSExecutor).lookupCNAME,
	"MX":    (*DNSExecutor).lookupMX,
	"NS":    (*DNSExecutor).lookupNS,
	"TXT":   (*DNSExecutor).lookupTXT,
	"PTR":   (*DNSExecutor).lookupPTR,
	"SRV":   (*DNSExecutor).lookupSRV,
	"CAA":   (*DNSExecutor).lookupCAA,
	"SOA":   (*DNSExecutor).lookupSOA,
}

func (d *DNSExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := d.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now(), time.Now())
	}
	cfg := cfgAny.(*DNSConfig)

	// The resolver's own timeouts come from resolv.conf, so bound the lookup
	// by the monitor timeout here.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
	defer cancel()

	// Create custom resolver with specified DNS server
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := d.dialer(&net.Dialer{
				Timeout: time.Duration(m.Timeout) * time.Second,
			})
			return dialer.DialContext(ctx, network, net.JoinHostPort(cfg.ResolverServer, strconv.Itoa(cfg.Port)))
		},
	}

	startTime := time.Now()
	message, err := d.lookup(ctx, dnsQuery{resolver: r, cfg: cfg, timeout: time.Duration(m.Timeout) * time.Second})
	endTime := time.Now()

	if err != nil {
		d.logger.Infof("DNS lookup failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("DNS lookup failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	if message == "" {
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("No %s records found for %s", cfg.ResolveType, cfg.Host),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	d.logger.Infof("DNS lookup successful: %s, %s", m.Name, message)

	return &Result{
		Status:    shared.MonitorStatusUp,
		Message:   message,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// lookup runs the lookup for the configured resolve type
func (d *DNSExecutor) lookup(ctx context.Context, q dnsQuery) (string, error) {
	lookup, ok := dnsLookups[strings.ToUpper(q.cfg.ResolveType)]
	if !ok {
		return "", fmt.Errorf("unsupported record type: %s", q.cfg.ResolveType)
	}
	return lookup(d, ctx, q)
}

func (d *DNSExecutor) lookupA(ctx context.Context, q dnsQuery) (string, error) {
	return lookupIPs(ctx, q, "ip4", "A")
}

func (d *DNSExecutor) lookupAAAA(ctx context.Context, q dnsQuery) (string, error) {
	return lookupIPs(ctx, q, "ip6", "AAAA")
}

func lookupIPs(ctx context.Context, q dnsQuery, network, recordType string) (string, error) {
	ips, err := q.resolver.LookupIP(ctx, network, q.cfg.Host)
	if err != nil || len(ips) == 0 {
		return "", err
	}
	ipStrings := make([]string, len(ips))
	for i, ip := range ips {
		ipStrings[i] = ip.String()
	}
	return fmt.Sprintf("%s records: %s", recordType, strings.Join(ipStrings, ", ")), nil
}

func (d *DNSExecutor) lookupCNAME(ctx context.Context, q dnsQuery) (string, error) {
	cname, err := q.resolver.LookupCNAME(ctx, q.cfg.Host)
	if err != nil || cname == "" {
		return "", err
	}
	return fmt.Sprintf("CNAME: %s", cname), nil
}

func (d *DNSExecutor) lookupMX(ctx context.Context, q dnsQuery) (string, error) {
	mxRecords, err := q.resolver.LookupMX(ctx, q.cfg.Host)
	if err != nil || len(mxRecords) == 0 {
		return "", err
	}
	mxStrings := make([]string, len(mxRecords))
	for i, mx := range mxRecords {
		mxStrings[i] = fmt.Sprintf("%s (priority: %d)", mx.Host, mx.Pref)
	}
	return fmt.Sprintf("MX records: %s", strings.Join(mxStrings, ", ")), nil
}

func (d *DNSExecutor) lookupNS(ctx context.Context, q dnsQuery) (string, error) {
	nsRecords, err := q.resolver.LookupNS(ctx, q.cfg.Host)
	if err != nil || len(nsRecords) == 0 {
		return "", err
	}
	nsStrings := make([]string, len(nsRecords))
	for i, ns := range nsRecords {
		nsStrings[i] = ns.Host
	}
	return fmt.Sprintf("NS records: %s", strings.Join(nsStrings, ", ")), nil
}

func (d *DNSExecutor) lookupTXT(ctx context.Context, q dnsQuery) (string, error) {
	txtRecords, err := q.resolver.LookupTXT(ctx, q.cfg.Host)
	if err != nil || len(txtRecords) == 0 {
		return "", err
	}
	return fmt.Sprintf("TXT records: %s", strings.Join(txtRecords, "; ")), nil
}

func (d *DNSExecutor) lookupPTR(ctx context.Context, q dnsQuery) (string, error) {
	// For PTR records, we need to reverse the IP address
	// This is a simplified version - in production you might want more robust handling
	names, err := q.resolver.LookupAddr(ctx, q.cfg.Host)
	if err != nil || len(names) == 0 {
		return "", err
	}
	return fmt.Sprintf("PTR records: %s", strings.Join(names, ", ")), nil
}

func (d *DNSExecutor) lookupSRV(ctx context.Context, q dnsQuery) (string, error) {
	// SRV records require a service and protocol, which we'll extract from the host
	// Format: _service._proto.name
	srvCname, srvRecords, err := q.resolver.LookupSRV(ctx, "", "", q.cfg.Host)
	if err != nil || len(srvRecords) == 0 {
		return "", err
	}
	srvStrings := make([]string, len(srvRecords))
	for i, srv := range srvRecords {
		srvStrings[i] = fmt.Sprintf("%s:%d (priority: %d, weight: %d)", srv.Target, srv.Port, srv.Priority, srv.Weight)
	}
	return fmt.Sprintf("SRV records (cname: %s): %s", srvCname, strings.Join(srvStrings, ", ")), nil
}

// exchange asks the resolver server directly, with miekg/dns, for records of one type
func (d *DNSExecutor) exchange(q dnsQuery, qtype uint16) (*dns.Msg, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(q.cfg.Host), qtype)

	client := new(dns.Client)
	client.Timeout = q.timeout
	client.Dialer = d.dialer(&net.Dialer{Timeout: client.Timeout})

	resp, _, err := client.Exchange(msg, net.JoinHostPort(q.cfg.ResolverServer, strconv.Itoa(q.cfg.Port)))
	return resp, err
}

func (d *DNSExecutor) lookupCAA(ctx context.Context, q dnsQuery) (string, error) {
	// Use miekg/dns for CAA record lookup
	resp, err := d.exchange(q, dns.TypeCAA)
	if err != nil || resp == nil {
		return "", err
	}

	var caaStrings []string
	for _, ans := range resp.Answer {
		if caa, ok := ans.(*dns.CAA); ok {
			caaStrings = append(caaStrings, fmt.Sprintf("%d %s %q", caa.Flag, caa.Tag, caa.Value))
		}
	}
	if len(caaStrings) == 0 {
		return "", nil
	}
	return fmt.Sprintf("CAA records: %s", strings.Join(caaStrings, "; ")), nil
}

func (d *DNSExecutor) lookupSOA(ctx context.Context, q dnsQuery) (string, error) {
	// Use miekg/dns for SOA record lookup
	resp, err := d.exchange(q, dns.TypeSOA)
	if err != nil || resp == nil {
		return "", err
	}

	for _, ans := range resp.Answer {
		if soa, ok := ans.(*dns.SOA); ok {
			return fmt.Sprintf("SOA: Primary NS: %s, Admin: %s, Serial: %d, Refresh: %d, Retry: %d, Expire: %d, Min TTL: %d",
				soa.Ns, soa.Mbox, soa.Serial, soa.Refresh, soa.Retry, soa.Expire, soa.Minttl), nil
		}
	}
	return "", nil
}
