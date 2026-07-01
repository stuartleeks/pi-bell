package onvif

import "testing"

func TestDetectOperation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "GetServices with soapenv prefix",
			body: `<?xml version="1.0"?><soapenv:Envelope xmlns:soapenv="http://www.w3.org/2003/05/soap-envelope"><soapenv:Header/><soapenv:Body><tds:GetServices xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:IncludeCapability>true</tds:IncludeCapability></tds:GetServices></soapenv:Body></soapenv:Envelope>`,
			want: "GetServices",
		},
		{
			name: "GetCapabilities with SOAP-ENV prefix",
			body: `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"><SOAP-ENV:Body><tds:GetCapabilities xmlns:tds="x"><tds:Category>All</tds:Category></tds:GetCapabilities></SOAP-ENV:Body></SOAP-ENV:Envelope>`,
			want: "GetCapabilities",
		},
		{
			name: "CreatePullPointSubscription no prefix",
			body: `<Envelope><Body><CreatePullPointSubscription><InitialTerminationTime>PT60S</InitialTerminationTime></CreatePullPointSubscription></Body></Envelope>`,
			want: "CreatePullPointSubscription",
		},
		{
			name: "PullMessages",
			body: `<soap:Envelope><soap:Body><tev:PullMessages xmlns:tev="http://www.onvif.org/ver10/events/wsdl"><tev:Timeout>PT30S</tev:Timeout><tev:MessageLimit>10</tev:MessageLimit></tev:PullMessages></soap:Body></soap:Envelope>`,
			want: "PullMessages",
		},
		{
			name: "no body",
			body: `<Envelope><Header/></Envelope>`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectOperation([]byte(tt.body))
			if got != tt.want {
				t.Errorf("detectOperation() = %q, want %q", got, tt.want)
			}
		})
	}
}
