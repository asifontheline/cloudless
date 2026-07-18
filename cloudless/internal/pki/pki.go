package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Cluster PKI: the first node mints a CA; joining nodes enroll their public
// key (authenticated by the cluster secret) and receive a signed cert. All
// peer-to-peer relay traffic then requires mutual TLS under this CA.

const (
	caCertFile   = "ca.pem"
	caKeyFile    = "ca.key"
	nodeCertFile = "node.pem"
	nodeKeyFile  = "node.key"
)

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), mode)
}

func readPEM(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	return block.Bytes, nil
}

// EnsureCA creates a cluster CA in dir if none exists.
func EnsureCA(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, caCertFile)); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "cloudless-cluster-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, caKeyFile), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return err
	}
	return writePEM(filepath.Join(dir, caCertFile), "CERTIFICATE", der, 0o644)
}

// SignPubKey signs a node's public key (DER) with the CA in dir and returns
// the node certificate in DER form. Only the CA-holding node can do this.
func SignPubKey(dir, nodeName string, pubDER []byte) ([]byte, error) {
	caDER, err := readPEM(filepath.Join(dir, caCertFile))
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}
	caKeyDER, err := readPEM(filepath.Join(dir, caKeyFile))
	if err != nil {
		return nil, err
	}
	caKey, err := x509.ParseECPrivateKey(caKeyDER)
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: nodeName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	return x509.CreateCertificate(rand.Reader, tmpl, caCert, pub, caKey)
}

// NewNodeKey generates the node key pair, storing the private key in dir and
// returning the public key DER for enrollment.
func NewNodeKey(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePEM(filepath.Join(dir, nodeKeyFile), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(&key.PublicKey)
}

// SaveNodeCreds stores the signed node cert and (for joiners) the CA cert.
func SaveNodeCreds(dir string, certDER, caDER []byte) error {
	if err := writePEM(filepath.Join(dir, nodeCertFile), "CERTIFICATE", certDER, 0o644); err != nil {
		return err
	}
	if caDER != nil {
		return writePEM(filepath.Join(dir, caCertFile), "CERTIFICATE", caDER, 0o644)
	}
	return nil
}

// SaveNodeCredsWithCA stores the signed node cert plus the CA cert already
// in PEM form (as received from enrollment).
func SaveNodeCredsWithCA(dir string, certDER, caPEM []byte) error {
	if err := writePEM(filepath.Join(dir, nodeCertFile), "CERTIFICATE", certDER, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, caCertFile), caPEM, 0o644)
}

// SelfIssue creates and stores this node's own cert (CA-holder path).
func SelfIssue(dir, nodeName string) error {
	if _, err := os.Stat(filepath.Join(dir, nodeCertFile)); err == nil {
		return nil
	}
	pub, err := NewNodeKey(dir)
	if err != nil {
		return err
	}
	der, err := SignPubKey(dir, nodeName, pub)
	if err != nil {
		return err
	}
	return SaveNodeCreds(dir, der, nil)
}

// CACertPEM returns the CA certificate PEM bytes.
func CACertPEM(dir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, caCertFile))
}

// HasCreds reports whether this node already holds a certificate.
func HasCreds(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, nodeCertFile))
	return err == nil
}

// verifyAgainstCA checks the presented chain against the cluster CA while
// skipping hostname verification: peers dial each other by ephemeral LAN IPs,
// so identity comes from possession of a CA-signed cert, not from SANs.
func verifyAgainstCA(pool *x509.CertPool) func(raw [][]byte, _ [][]*x509.Certificate) error {
	return func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) == 0 {
			return errors.New("no peer certificate")
		}
		cert, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return err
		}
		inter := x509.NewCertPool()
		for _, der := range raw[1:] {
			if c, err := x509.ParseCertificate(der); err == nil {
				inter.AddCert(c)
			}
		}
		_, err = cert.Verify(x509.VerifyOptions{
			Roots: pool, Intermediates: inter,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		})
		return err
	}
}

func caPool(dir string) (*x509.CertPool, error) {
	pemBytes, err := CACertPEM(dir)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("bad CA cert")
	}
	return pool, nil
}

func nodeCert(dir string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(filepath.Join(dir, nodeCertFile), filepath.Join(dir, nodeKeyFile))
}

// ServerTLS returns the relay server config: presents the node cert and
// requires a CA-signed client cert (mutual TLS).
func ServerTLS(dir string) (*tls.Config, error) {
	cert, err := nodeCert(dir)
	if err != nil {
		return nil, err
	}
	pool, err := caPool(dir)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		ClientAuth:            tls.RequireAnyClientCert,
		VerifyPeerCertificate: verifyAgainstCA(pool),
		MinVersion:            tls.VersionTLS13,
	}, nil
}

// ClientTLS returns the client config for dialing peer relays.
func ClientTLS(dir string) (*tls.Config, error) {
	cert, err := nodeCert(dir)
	if err != nil {
		return nil, err
	}
	pool, err := caPool(dir)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true, // chain is fully verified below against the cluster CA; only hostname matching is skipped
		VerifyPeerCertificate: verifyAgainstCA(pool),
		MinVersion:            tls.VersionTLS13,
	}, nil
}
