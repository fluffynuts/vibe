package signin

import "testing"

func TestADeviceLinkIsFoundByItsShapeNotByTheWordsAroundIt(t *testing.T) {
	cases := map[string]string{
		"English":          "not signed in to Docker; starting the sign-in flow…\nYour one-time device confirmation code is: FCXR-MHDV\nOpen this URL to sign in: https://login.docker.com/activate?user_code=FCXR-MHDV\n\nWaiting for authentication...",
		"another language": "Ouvrez cette adresse : https://login.docker.com/activate?user_code=FCXR-MHDV (puis revenez)",
	}
	for name, text := range cases {
		link, ok := DeviceLink(text)
		if !ok || link.URL != "https://login.docker.com/activate?user_code=FCXR-MHDV" || link.Code != "FCXR-MHDV" {
			t.Errorf("%s: got %+v, %v", name, link, ok)
		}
	}
}

func TestOnlyADockerAddressWithAUserCodeIsADeviceLink(t *testing.T) {
	for _, text := range []string{
		"",
		"https://www.docker.com/legal/docker-subscription-service-agreement/",
		"https://login.docker.com/activate",
		"https://evil.example.com/activate?user_code=ABCD-EFGH",
		"https://notdocker.com/activate?user_code=ABCD-EFGH",
		"http://login.docker.com/activate?user_code=ABCD-EFGH",
	} {
		if link, ok := DeviceLink(text); ok {
			t.Errorf("%q was taken for a device link: %+v", text, link)
		}
	}
}
