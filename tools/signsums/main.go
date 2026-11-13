// Command signsums signs the checksum file of a release, or creates the key.
//
//	signsums -gen <keyfile>   new key pair: private key into keyfile, public key on stdout
//	signsums <file>           writes <file>.sig, key from $RELEASE_SIGNING_KEY (base64)
//
// The private key belongs in the build secrets only; easyTTY carries the
// public key and refuses updates whose checksums it did not sign.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"

	"easytty/internal/update"
)

func main() {
	gen := flag.String("gen", "", "create a key pair and write the private key to this file")
	flag.Parse()
	var err error
	switch {
	case *gen != "":
		err = generate(*gen)
	case flag.NArg() == 1:
		err = sign(flag.Arg(0))
	default:
		err = errors.New("Aufruf: signsums -gen <schlüsseldatei> | signsums <datei>")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "signsums:", err)
		os.Exit(1)
	}
}

func generate(path string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // never overwrite a key
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(path string) error {
	b64 := os.Getenv("RELEASE_SIGNING_KEY")
	if b64 == "" {
		return errors.New("RELEASE_SIGNING_KEY ist leer – ohne Schlüssel kein Release")
	}
	key, err := update.ParsePrivateKey(b64)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".sig", update.Sign(key, data), 0o644)
}
