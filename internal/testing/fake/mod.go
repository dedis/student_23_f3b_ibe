// Package fake provides fake implementations for interfaces commonly used in
// the repository.
// The implementations offer configuration to return errors when it is needed by
// the unit test and it is also possible to record the call of functions of an
// object in some cases.
package fake

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/dela/crypto"
	"go.dedis.ch/dela/mino"
	"go.dedis.ch/dela/serde"
	"golang.org/x/xerrors"
)

// Call is a tool to keep track of a function calls.
type Call struct {
	sync.Mutex
	calls [][]interface{}
}

// Get returns the nth call ith parameter.
func (c *Call) Get(n, i int) interface{} {
	if c == nil {
		return nil
	}

	c.Lock()
	defer c.Unlock()

	return c.calls[n][i]
}

// Len returns the number of calls.
func (c *Call) Len() int {
	if c == nil {
		return 0
	}

	c.Lock()
	defer c.Unlock()

	return len(c.calls)
}

// Add adds a call to the list.
func (c *Call) Add(args ...interface{}) {
	if c == nil {
		return
	}

	c.Lock()
	defer c.Unlock()

	c.calls = append(c.calls, args)
}

// Clear clears the array of calls.
func (c *Call) Clear() {
	if c != nil {
		c.Lock()
		c.calls = nil
		c.Unlock()
	}
}

// Address is a fake implementation of mino.Address
type Address struct {
	mino.Address
	index int
	err   error
}

// NewAddress returns a fake address with the given index.
func NewAddress(index int) Address {
	return Address{index: index}
}

// NewBadAddress returns a fake address that returns an error when appropriate.
func NewBadAddress() Address {
	return Address{err: xerrors.New("fake error")}
}

// Equal implements mino.Address.
func (a Address) Equal(o mino.Address) bool {
	other, ok := o.(Address)
	return ok && other.index == a.index
}

// MarshalText implements encoding.TextMarshaler.
func (a Address) MarshalText() ([]byte, error) {
	buffer := make([]byte, 4)
	binary.LittleEndian.PutUint32(buffer, uint32(a.index))
	return buffer, a.err
}

func (a Address) String() string {
	return fmt.Sprintf("fake.Address[%d]", a.index)
}

// AddressIterator is a fake implementation of the mino.AddressIterator
// interface.
type AddressIterator struct {
	mino.AddressIterator
	addrs []mino.Address
	index int
}

// NewAddressIterator returns a new address iterator
func NewAddressIterator(addrs []mino.Address) *AddressIterator {
	return &AddressIterator{
		addrs: addrs,
	}
}

// Seek implements mino.AddressIterator.
func (i *AddressIterator) Seek(index int) {
	i.index = index
}

// HasNext implements mino.AddressIterator.
func (i *AddressIterator) HasNext() bool {
	return i.index < len(i.addrs)
}

// GetNext implements mino.AddressIterator.
func (i *AddressIterator) GetNext() mino.Address {
	res := i.addrs[i.index]
	i.index++
	return res
}

// PublicKeyIterator is a fake implementation of crypto.PublicKeyIterator.
type PublicKeyIterator struct {
	signers []crypto.Signer
	index   int
}

// NewPublicKeyIterator returns a new address iterator
func NewPublicKeyIterator(signers []crypto.Signer) *PublicKeyIterator {
	return &PublicKeyIterator{
		signers: signers,
	}
}

// Seek implements crypto.PublicKeyIterator.
func (i *PublicKeyIterator) Seek(index int) {
	i.index = index
}

// HasNext implements crypto.PublicKeyIterator.
func (i *PublicKeyIterator) HasNext() bool {
	return i.index < len(i.signers)
}

// GetNext implements crypto.PublicKeyIterator.
func (i *PublicKeyIterator) GetNext() crypto.PublicKey {
	if i.HasNext() {
		res := i.signers[i.index]
		i.index++
		return res.GetPublicKey()
	}
	return nil
}

// CollectiveAuthority is a fake implementation of the cosi.CollectiveAuthority
// interface.
type CollectiveAuthority struct {
	crypto.CollectiveAuthority
	addrs   []mino.Address
	signers []crypto.Signer

	Call           *Call
	PubkeyNotFound bool
}

// GenSigner is a function to generate a signer.
type GenSigner func() crypto.Signer

// NewAuthority returns a new collective authority of n members with new signers
// generated by g.
func NewAuthority(n int, g GenSigner) CollectiveAuthority {
	return NewAuthorityWithBase(0, n, g)
}

// NewAuthorityWithBase returns a new fake collective authority of size n with
// a given starting base index.
func NewAuthorityWithBase(base int, n int, g GenSigner) CollectiveAuthority {
	signers := make([]crypto.Signer, n)
	for i := range signers {
		signers[i] = g()
	}

	addrs := make([]mino.Address, n)
	for i := range addrs {
		addrs[i] = Address{index: i + base}
	}

	return CollectiveAuthority{
		signers: signers,
		addrs:   addrs,
	}
}

// NewAuthorityFromMino returns a new fake collective authority using
// the addresses of the Mino instances.
func NewAuthorityFromMino(g GenSigner, instances ...mino.Mino) CollectiveAuthority {
	signers := make([]crypto.Signer, len(instances))
	for i := range signers {
		signers[i] = g()
	}

	addrs := make([]mino.Address, len(instances))
	for i, instance := range instances {
		addrs[i] = instance.GetAddress()
	}

	return CollectiveAuthority{
		signers: signers,
		addrs:   addrs,
	}
}

// GetAddress returns the address at the provided index.
func (ca CollectiveAuthority) GetAddress(index int) mino.Address {
	return ca.addrs[index]
}

// GetSigner returns the signer at the provided index.
func (ca CollectiveAuthority) GetSigner(index int) crypto.Signer {
	return ca.signers[index]
}

// GetPublicKey implements cosi.CollectiveAuthority.
func (ca CollectiveAuthority) GetPublicKey(addr mino.Address) (crypto.PublicKey, int) {
	if ca.PubkeyNotFound {
		return nil, -1
	}

	for i, address := range ca.addrs {
		if address.Equal(addr) {
			return ca.signers[i].GetPublicKey(), i
		}
	}
	return nil, -1
}

// Take implements mino.Players.
func (ca CollectiveAuthority) Take(updaters ...mino.FilterUpdater) mino.Players {
	filter := mino.ApplyFilters(updaters)
	newCA := CollectiveAuthority{
		Call:    ca.Call,
		addrs:   make([]mino.Address, len(filter.Indices)),
		signers: make([]crypto.Signer, len(filter.Indices)),
	}
	for i, k := range filter.Indices {
		newCA.addrs[i] = ca.addrs[k]
		newCA.signers[i] = ca.signers[k]
	}
	return newCA
}

// Len implements mino.Players.
func (ca CollectiveAuthority) Len() int {
	return len(ca.signers)
}

// AddressIterator implements mino.Players.
func (ca CollectiveAuthority) AddressIterator() mino.AddressIterator {
	return &AddressIterator{addrs: ca.addrs}
}

// PublicKeyIterator implements cosi.CollectiveAuthority.
func (ca CollectiveAuthority) PublicKeyIterator() crypto.PublicKeyIterator {
	return &PublicKeyIterator{signers: ca.signers}
}

// PublicKeyFactory is a fake implementation of a public key factory.
type PublicKeyFactory struct {
	pubkey PublicKey
	err    error
}

// NewPublicKeyFactory returns a fake public key factory that returns the given
// public key.
func NewPublicKeyFactory(pubkey PublicKey) PublicKeyFactory {
	return PublicKeyFactory{
		pubkey: pubkey,
	}
}

// NewBadPublicKeyFactory returns a fake public key factory that returns an
// error when appropriate.
func NewBadPublicKeyFactory() PublicKeyFactory {
	return PublicKeyFactory{err: xerrors.New("fake error")}
}

// Deserialize implements serde.Factory.
func (f PublicKeyFactory) Deserialize(serde.Context, []byte) (serde.Message, error) {
	return f.pubkey, f.err
}

// PublicKeyOf implements crypto.PublicKeyFactory.
func (f PublicKeyFactory) PublicKeyOf(serde.Context, []byte) (crypto.PublicKey, error) {
	return f.pubkey, f.err
}

// SignatureByte is the byte returned when marshaling a fake signature.
const SignatureByte = 0xfe

// Signature is a fake implementation of the signature.
type Signature struct {
	crypto.Signature
	err error
}

// NewBadSignature returns a signature that will return error when appropriate.
func NewBadSignature() Signature {
	return Signature{err: xerrors.New("fake error")}
}

// Equal implements crypto.Signature.
func (s Signature) Equal(o crypto.Signature) bool {
	_, ok := o.(Signature)
	return ok
}

// Serialize implements serde.Message.
func (s Signature) Serialize(serde.Context) ([]byte, error) {
	return []byte("{}"), s.err
}

// MarshalBinary implements crypto.Signature.
func (s Signature) MarshalBinary() ([]byte, error) {
	return []byte{SignatureByte}, s.err
}

// SignatureFactory is a fake implementation of the signature factory.
type SignatureFactory struct {
	Counter   *Counter
	signature Signature
	err       error
}

// NewSignatureFactory returns a fake signature factory.
func NewSignatureFactory(s Signature) SignatureFactory {
	return SignatureFactory{signature: s}
}

// NewBadSignatureFactory returns a signature factory that will return an error
// when appropriate.
func NewBadSignatureFactory() SignatureFactory {
	return SignatureFactory{
		err: xerrors.New("fake error"),
	}
}

func NewBadSignatureFactoryWithDelay(value int) SignatureFactory {
	return SignatureFactory{
		err:     xerrors.New("fake error"),
		Counter: &Counter{Value: value},
	}
}

// Deserialize implements serde.Factory.
func (f SignatureFactory) Deserialize(ctx serde.Context, data []byte) (serde.Message, error) {
	return f.SignatureOf(ctx, data)
}

// SignatureOf implements crypto.SignatureFactory.
func (f SignatureFactory) SignatureOf(serde.Context, []byte) (crypto.Signature, error) {
	if !f.Counter.Done() {
		f.Counter.Decrease()
		return f.signature, nil
	}
	return f.signature, f.err
}

// PublicKey is a fake implementation of crypto.PublicKey.
type PublicKey struct {
	crypto.PublicKey
	err       error
	verifyErr error
}

// NewBadPublicKey returns a new fake public key that returns error when
// appropriate.
func NewBadPublicKey() PublicKey {
	return PublicKey{
		err:       xerrors.New("fake error"),
		verifyErr: xerrors.New("fake error"),
	}
}

// NewInvalidPublicKey returns a fake public key that never verifies.
func NewInvalidPublicKey() PublicKey {
	return PublicKey{verifyErr: xerrors.New("fake error")}
}

// Verify implements crypto.PublicKey.
func (pk PublicKey) Verify([]byte, crypto.Signature) error {
	return pk.verifyErr
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (pk PublicKey) MarshalBinary() ([]byte, error) {
	return []byte{0xdf}, pk.err
}

// Serialize implements serde.Message.
func (pk PublicKey) Serialize(serde.Context) ([]byte, error) {
	return []byte(`{}`), pk.err
}

// String implements fmt.Stringer.
func (pk PublicKey) String() string {
	return "fake.PublicKey"
}

// Signer is a fake implementation of the crypto.AggregateSigner interface.
type Signer struct {
	crypto.AggregateSigner
	publicKey        PublicKey
	signatureFactory SignatureFactory
	verifierFactory  VerifierFactory
	err              error
}

// NewSigner returns a new instance of the fake signer.
func NewSigner() crypto.Signer {
	return Signer{}
}

// NewAggregateSigner returns a new signer that implements aggregation.
func NewAggregateSigner() Signer {
	return Signer{}
}

// NewSignerWithSignatureFactory returns a fake signer with the provided
// factory.
func NewSignerWithSignatureFactory(f SignatureFactory) Signer {
	return Signer{signatureFactory: f}
}

// NewSignerWithVerifierFactory returns a new fake signer with the specific
// verifier factory.
func NewSignerWithVerifierFactory(f VerifierFactory) Signer {
	return Signer{verifierFactory: f}
}

// NewSignerWithPublicKey returns a new fake signer with the specific public
// key.
func NewSignerWithPublicKey(k PublicKey) Signer {
	return Signer{publicKey: k}
}

// NewBadSigner returns a fake signer that will return an error when
// appropriate.
func NewBadSigner() Signer {
	return Signer{err: xerrors.New("fake error")}
}

// GetPublicKeyFactory implements crypto.Signer.
func (s Signer) GetPublicKeyFactory() crypto.PublicKeyFactory {
	return PublicKeyFactory{}
}

// GetSignatureFactory implements crypto.Signer.
func (s Signer) GetSignatureFactory() crypto.SignatureFactory {
	return s.signatureFactory
}

// GetVerifierFactory implements crypto.Signer.
func (s Signer) GetVerifierFactory() crypto.VerifierFactory {
	return s.verifierFactory
}

// GetPublicKey implements crypto.Signer.
func (s Signer) GetPublicKey() crypto.PublicKey {
	return s.publicKey
}

// Sign implements crypto.Signer.
func (s Signer) Sign([]byte) (crypto.Signature, error) {
	return Signature{}, s.err
}

// Aggregate implements crypto.AggregateSigner.
func (s Signer) Aggregate(...crypto.Signature) (crypto.Signature, error) {
	return Signature{}, s.err
}

// Verifier is a fake implementation of crypto.Verifier.
type Verifier struct {
	crypto.Verifier
	err error
}

// NewBadVerifier returns a verifier that will return an error when appropriate.
func NewBadVerifier() Verifier {
	return Verifier{err: xerrors.New("fake error")}
}

// Verify implements crypto.Verifier.
func (v Verifier) Verify(msg []byte, s crypto.Signature) error {
	return v.err
}

// VerifierFactory is a fake implementation of crypto.VerifierFactory.
type VerifierFactory struct {
	crypto.VerifierFactory
	verifier Verifier
	err      error
	call     *Call
}

// NewVerifierFactory returns a new fake verifier factory.
func NewVerifierFactory(v Verifier) VerifierFactory {
	return VerifierFactory{verifier: v}
}

// NewVerifierFactoryWithCalls returns a new verifier factory that will register
// the calls.
func NewVerifierFactoryWithCalls(c *Call) VerifierFactory {
	return VerifierFactory{call: c}
}

// NewBadVerifierFactory returns a fake verifier factory that returns an error
// when appropriate.
func NewBadVerifierFactory() VerifierFactory {
	return VerifierFactory{err: xerrors.New("fake error")}
}

// FromAuthority implements crypto.VerifierFactory.
func (f VerifierFactory) FromAuthority(ca crypto.CollectiveAuthority) (crypto.Verifier, error) {
	if f.call != nil {
		f.call.Add(ca)
	}
	return f.verifier, f.err
}

// Counter is a helper to delay errors or actions. It can be nil without panics.
type Counter struct {
	Value int
}

// NewCounter returns a new counter set to the given value.
func NewCounter(value int) *Counter {
	return &Counter{
		Value: value,
	}
}

// Done returns true when the counter reached zero.
func (c *Counter) Done() bool {
	return c == nil || c.Value <= 0
}

// Decrease decrements the counter.
func (c *Counter) Decrease() {
	if c == nil {
		return
	}
	c.Value--
}

// AddressFactory is a fake implementation of mino.AddressFactory.
type AddressFactory struct {
	mino.AddressFactory
}

// FromText implements mino.AddressFactory.
func (f AddressFactory) FromText(text []byte) mino.Address {
	if len(text) >= 4 {
		index := binary.LittleEndian.Uint32(text)
		return Address{index: int(index)}
	}
	return Address{}
}

// NewReceiver returns a new receiver
func NewReceiver(Msg ...serde.Message) Receiver {
	return Receiver{
		Msg: Msg,
	}
}

// Receiver is a fake RPC stream receiver. It will return the consecutive
// messages stored in the Msg slice.
type Receiver struct {
	mino.Receiver
	err   error
	Msg   []serde.Message
	index int
}

// NewBadReceiver returns a new receiver that returns an error.
func NewBadReceiver() Receiver {
	return Receiver{err: xerrors.New("fake error")}
}

// Recv implements mino.Receiver.
func (r *Receiver) Recv(context.Context) (mino.Address, serde.Message, error) {
	if r.Msg == nil {
		return nil, nil, r.err
	}

	// In the case there are no more messages to read we return the last one
	if r.index >= len(r.Msg) {
		return nil, r.Msg[len(r.Msg)-1], r.err
	}

	defer func() {
		r.index++
	}()
	return nil, r.Msg[r.index], r.err
}

// Sender is a fake RPC stream sender.
type Sender struct {
	mino.Sender
	err error
}

// NewBadSender returns a sender that always returns an error.
func NewBadSender() Sender {
	return Sender{err: xerrors.New("fake error")}
}

// Send implements mino.Sender.
func (s Sender) Send(serde.Message, ...mino.Address) <-chan error {
	errs := make(chan error, 1)
	errs <- s.err
	close(errs)
	return errs
}

// RPC is a fake implementation of mino.RPC.
type RPC struct {
	mino.RPC
	Calls    *Call
	msgs     chan mino.Response
	receiver *Receiver
	sender   Sender
	err      error
}

// NewRPC returns a fake rpc.
func NewRPC() *RPC {
	rpc := &RPC{}
	rpc.Reset()
	return rpc
}

// NewStreamRPC returns a fake rpc with specific stream options.
func NewStreamRPC(r Receiver, s Sender) *RPC {
	rpc := &RPC{
		receiver: &r,
		sender:   s,
	}
	rpc.Reset()
	return rpc
}

// NewBadRPC returns a fake rpc that returns an error when appropriate.
func NewBadRPC() *RPC {
	rpc := &RPC{
		err: xerrors.New("fake error"),
	}
	rpc.Reset()
	return rpc
}

func (rpc *RPC) SendResponse(from mino.Address, msg serde.Message) {
	rpc.msgs <- mino.NewResponse(from, msg)
}

func (rpc *RPC) SendResponseWithError(from mino.Address, err error) {
	rpc.msgs <- mino.NewResponseWithError(from, err)
}

func (rpc *RPC) Done() {
	close(rpc.msgs)
}

// Call implements mino.RPC.
func (rpc *RPC) Call(ctx context.Context,
	m serde.Message, p mino.Players) (<-chan mino.Response, error) {

	rpc.Calls.Add(ctx, m, p)

	return rpc.msgs, rpc.err
}

// Stream implements mino.RPC.
func (rpc *RPC) Stream(ctx context.Context, p mino.Players) (mino.Sender, mino.Receiver, error) {
	rpc.Calls.Add(ctx, p)

	return rpc.sender, rpc.receiver, rpc.err
}

// Reset resets the channels.
func (rpc *RPC) Reset() {
	rpc.Calls = &Call{}
	rpc.msgs = make(chan mino.Response, 100)
}

// Mino is a fake implementation of mino.Mino.
type Mino struct {
	mino.Mino
	err error
}

// NewBadMino returns a Mino instance that returns an error when appropriate.
func NewBadMino() Mino {
	return Mino{err: xerrors.New("fake error")}
}

// GetAddress implements mino.Mino.
func (m Mino) GetAddress() mino.Address {
	return Address{}
}

// GetAddressFactory implements mino.Mino.
func (m Mino) GetAddressFactory() mino.AddressFactory {
	return AddressFactory{}
}

// MakeRPC implements mino.Mino.
func (m Mino) MakeRPC(string, mino.Handler, serde.Factory) (mino.RPC, error) {
	return NewRPC(), m.err
}

// Hash is a fake implementation of hash.Hash.
type Hash struct {
	hash.Hash
	delay int
	err   error
	Call  *Call
}

// NewBadHash returns a fake hash that returns an error when appropriate.
func NewBadHash() *Hash {
	return &Hash{err: xerrors.New("fake error")}
}

// NewBadHashWithDelay returns a fake hash that returns an error after a certain
// amount of calls.
func NewBadHashWithDelay(delay int) *Hash {
	return &Hash{err: xerrors.New("fake error"), delay: delay}
}

func (h *Hash) Write(in []byte) (int, error) {
	if h.Call != nil {
		h.Call.Add(in)
	}

	if h.delay > 0 {
		h.delay--
		return 0, nil
	}
	return 0, h.err
}

// Size implements hash.Hash.
func (h *Hash) Size() int {
	return 32
}

// Sum implements hash.Hash.
func (h *Hash) Sum([]byte) []byte {
	return make([]byte, 32)
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (h *Hash) MarshalBinary() ([]byte, error) {
	if h.delay > 0 {
		h.delay--
		return []byte{}, nil
	}
	return []byte{}, h.err
}

// UnmarshalBinary implements encodi8ng.BinaryUnmarshaler.
func (h *Hash) UnmarshalBinary([]byte) error {
	if h.delay > 0 {
		h.delay--
		return nil
	}
	return h.err
}

// HashFactory is a fake implementation of crypto.HashFactory.
type HashFactory struct {
	hash *Hash
}

// NewHashFactory returns a fake hash factory.
func NewHashFactory(h *Hash) HashFactory {
	return HashFactory{hash: h}
}

// New implements crypto.HashFactory.
func (f HashFactory) New() hash.Hash {
	return f.hash
}

// MakeCertificate generates a valid certificate for the localhost address and
// for an hour.
func MakeCertificate(t *testing.T, n int) *tls.Certificate {
	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	buf, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(buf)
	require.NoError(t, err)

	chain := make([][]byte, n)
	for i := range chain {
		chain[i] = buf
	}

	return &tls.Certificate{
		Certificate: chain,
		PrivateKey:  priv,
		Leaf:        cert,
	}
}

// Message is a fake implementation if a serde message.
type Message struct {
	Digest []byte
}

// Fingerprint implements serde.Fingerprinter.
func (m Message) Fingerprint(w io.Writer) error {
	w.Write(m.Digest)
	return nil
}

// Serialize implements serde.Message.
func (m Message) Serialize(ctx serde.Context) ([]byte, error) {
	return ctx.Marshal(struct{}{})
}

// MessageFactory is a fake implementation of a serde factory.
type MessageFactory struct {
	err error
}

func NewBadMessageFactory() MessageFactory {
	return MessageFactory{
		err: xerrors.New("fake error"),
	}
}

// Deserialize implements serde.Factory.
func (f MessageFactory) Deserialize(ctx serde.Context, data []byte) (serde.Message, error) {
	return Message{}, f.err
}

const (
	GoodFormat = serde.Format("FakeGood")
	BadFormat  = serde.Format("FakeBad")
)

type Format struct {
	err  error
	Msg  serde.Message
	Call *Call
}

func NewBadFormat() Format {
	return Format{err: xerrors.New("fake error")}
}

func (f Format) Encode(ctx serde.Context, m serde.Message) ([]byte, error) {
	f.Call.Add(ctx, m)
	return []byte("fake format"), f.err
}

func (f Format) Decode(ctx serde.Context, data []byte) (serde.Message, error) {
	f.Call.Add(ctx, data)
	return f.Msg, f.err
}

type ContextEngine struct {
	Count  *Counter
	format serde.Format
	err    error
}

func NewContext() serde.Context {
	return serde.NewContext(ContextEngine{
		format: GoodFormat,
	})
}

func NewContextWithFormat(f serde.Format) serde.Context {
	return serde.NewContext(ContextEngine{
		format: f,
	})
}

func NewBadContext() serde.Context {
	return serde.NewContext(ContextEngine{
		format: BadFormat,
		err:    xerrors.New("fake error"),
	})
}

func NewBadContextWithDelay(delay int) serde.Context {
	return serde.NewContext(ContextEngine{
		Count:  &Counter{Value: delay},
		format: BadFormat,
		err:    xerrors.New("fake error"),
	})
}

func (ctx ContextEngine) GetFormat() serde.Format {
	return ctx.format
}

func (ctx ContextEngine) Marshal(m interface{}) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	if !ctx.Count.Done() {
		ctx.Count.Decrease()
		return data, nil
	}

	return data, ctx.err
}

func (ctx ContextEngine) Unmarshal(data []byte, m interface{}) error {
	err := json.Unmarshal(data, m)
	if err != nil {
		return err
	}

	if !ctx.Count.Done() {
		ctx.Count.Decrease()
		return nil
	}

	return ctx.err
}
