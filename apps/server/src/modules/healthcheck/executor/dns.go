package executor

import (
	"context"
	"fmt"
	"net"
	"peekaping/src/modules/shared"
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

func (d *DNSExecutor) Execute(ctx context.Context, m *Monitor, proxyModel *Proxy) *Result {
	cfgAny, err := d.Unmarshal(m.Config)
	if err != nil {
		return DownResult(err, time.Now().UTC(), time.Now().UTC())
	}
	cfg := cfgAny.(*DNSConfig)

	d.logger.Debugf("execute dns cfg: %+v", cfg)

	// Create custom resolver with specified DNS server
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: time.Duration(m.Timeout) * time.Second,
			}
			return dialer.DialContext(ctx, network, fmt.Sprintf("%s:%d", cfg.ResolverServer, cfg.Port))
		},
	}

	startTime := time.Now().UTC()
	var recordsFound bool
	var message string

	switch strings.ToUpper(cfg.ResolveType) {
	case "A":
		var ips []net.IP
		ips, err = r.LookupIP(ctx, "ip4", cfg.Host)
		if err == nil && len(ips) > 0 {
			recordsFound = true
			ipStrings := make([]string, len(ips))
			for i, ip := range ips {
				ipStrings[i] = ip.String()
			}
			message = fmt.Sprintf("A records: %s", strings.Join(ipStrings, ", "))
		}
	case "AAAA":
		var ips []net.IP
		ips, err = r.LookupIP(ctx, "ip6", cfg.Host)
		if err == nil && len(ips) > 0 {
			recordsFound = true
			ipStrings := make([]string, len(ips))
			for i, ip := range ips {
				ipStrings[i] = ip.String()
			}
			message = fmt.Sprintf("AAAA records: %s", strings.Join(ipStrings, ", "))
		}
	case "CNAME":
		var cname string
		cname, err = r.LookupCNAME(ctx, cfg.Host)
		if err == nil && cname != "" {
			recordsFound = true
			message = fmt.Sprintf("CNAME: %s", cname)
		}
	case "MX":
		var mxRecords []*net.MX
		mxRecords, err = r.LookupMX(ctx, cfg.Host)
		if err == nil && len(mxRecords) > 0 {
			recordsFound = true
			mxStrings := make([]string, len(mxRecords))
			for i, mx := range mxRecords {
				mxStrings[i] = fmt.Sprintf("%s (priority: %d)", mx.Host, mx.Pref)
			}
			message = fmt.Sprintf("MX records: %s", strings.Join(mxStrings, ", "))
		}
	case "NS":
		var nsRecords []*net.NS
		nsRecords, err = r.LookupNS(ctx, cfg.Host)
		if err == nil && len(nsRecords) > 0 {
			recordsFound = true
			nsStrings := make([]string, len(nsRecords))
			for i, ns := range nsRecords {
				nsStrings[i] = ns.Host
			}
			message = fmt.Sprintf("NS records: %s", strings.Join(nsStrings, ", "))
		}
	case "TXT":
		var txtRecords []string
		txtRecords, err = r.LookupTXT(ctx, cfg.Host)
		if err == nil && len(txtRecords) > 0 {
			recordsFound = true
			message = fmt.Sprintf("TXT records: %s", strings.Join(txtRecords, "; "))
		}
	case "PTR":
		// For PTR records, we need to reverse the IP address
		// This is a simplified version - in production you might want more robust handling
		var names []string
		names, err = r.LookupAddr(ctx, cfg.Host)
		if err == nil && len(names) > 0 {
			recordsFound = true
			message = fmt.Sprintf("PTR records: %s", strings.Join(names, ", "))
		}
	case "SRV":
		// SRV records require a service and protocol, which we'll extract from the host
		// Format: _service._proto.name
		var srvCname string
		var srvRecords []*net.SRV
		srvCname, srvRecords, err = r.LookupSRV(ctx, "", "", cfg.Host)
		if err == nil && len(srvRecords) > 0 {
			recordsFound = true
			srvStrings := make([]string, len(srvRecords))
			for i, srv := range srvRecords {
				srvStrings[i] = fmt.Sprintf("%s:%d (priority: %d, weight: %d)", srv.Target, srv.Port, srv.Priority, srv.Weight)
			}
			message = fmt.Sprintf("SRV records (cname: %s): %s", srvCname, strings.Join(srvStrings, ", "))
		}
	case "CAA":
		// Use miekg/dns for CAA record lookup
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(cfg.Host), dns.TypeCAA)

		client := new(dns.Client)
		client.Timeout = time.Duration(m.Timeout) * time.Second

		var resp *dns.Msg
		resp, _, err = client.Exchange(msg, fmt.Sprintf("%s:%d", cfg.ResolverServer, cfg.Port))

		if err == nil && resp != nil && len(resp.Answer) > 0 {
			var caaStrings []string
			for _, ans := range resp.Answer {
				if caa, ok := ans.(*dns.CAA); ok {
					caaStrings = append(caaStrings, fmt.Sprintf("%d %s %q", caa.Flag, caa.Tag, caa.Value))
				}
			}
			if len(caaStrings) > 0 {
				recordsFound = true
				message = fmt.Sprintf("CAA records: %s", strings.Join(caaStrings, "; "))
			}
		}
	case "SOA":
		// Use miekg/dns for SOA record lookup
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(cfg.Host), dns.TypeSOA)

		client := new(dns.Client)
		client.Timeout = time.Duration(m.Timeout) * time.Second

		var resp *dns.Msg
		resp, _, err = client.Exchange(msg, fmt.Sprintf("%s:%d", cfg.ResolverServer, cfg.Port))

		if err == nil && resp != nil && len(resp.Answer) > 0 {
			for _, ans := range resp.Answer {
				if soa, ok := ans.(*dns.SOA); ok {
					recordsFound = true
					message = fmt.Sprintf("SOA: Primary NS: %s, Admin: %s, Serial: %d, Refresh: %d, Retry: %d, Expire: %d, Min TTL: %d",
						soa.Ns, soa.Mbox, soa.Serial, soa.Refresh, soa.Retry, soa.Expire, soa.Minttl)
					break
				}
			}
		}
	default:
		err = fmt.Errorf("unsupported record type: %s", cfg.ResolveType)
	}

	endTime := time.Now().UTC()

	if err != nil {
		d.logger.Infof("DNS lookup failed: %s, %s", m.Name, err.Error())
		return &Result{
			Status:    shared.MonitorStatusDown,
			Message:   fmt.Sprintf("DNS lookup failed: %v", err),
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	if !recordsFound {
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
