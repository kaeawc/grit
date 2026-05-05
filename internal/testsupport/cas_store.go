package testsupport

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/kaeawc/grit/internal/cas"
)

type CASStoreCall struct {
	Op         string
	Hash       cas.Hash
	ActionHash cas.Hash
	Size       int64
}

type CASStoreRecorder struct {
	mu          sync.Mutex
	Calls       []CASStoreCall
	Blobs       map[cas.Hash][]byte
	Provenances map[cas.Hash]cas.Provenance
	Results     map[cas.Hash]cas.ActionResult
	Err         error
}

func NewCASStoreRecorder() *CASStoreRecorder {
	return &CASStoreRecorder{
		Blobs:       map[cas.Hash][]byte{},
		Provenances: map[cas.Hash]cas.Provenance{},
		Results:     map[cas.Hash]cas.ActionResult{},
	}
}

func (s *CASStoreRecorder) Put(ctx context.Context, r io.Reader, prov cas.Provenance) (cas.BlobInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return cas.BlobInfo{}, err
	}
	return s.PutBytes(ctx, data, prov)
}

func (s *CASStoreRecorder) PutBytes(ctx context.Context, data []byte, prov cas.Provenance) (cas.BlobInfo, error) {
	hash := cas.HashBytes(data)
	return s.PutBytesExpected(ctx, data, hash, prov)
}

func (s *CASStoreRecorder) PutExpected(ctx context.Context, r io.Reader, expected cas.Hash, prov cas.Provenance) (cas.BlobInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return cas.BlobInfo{}, err
	}
	return s.PutBytesExpected(ctx, data, expected, prov)
}

func (s *CASStoreRecorder) PutBytesExpected(ctx context.Context, data []byte, expected cas.Hash, prov cas.Provenance) (cas.BlobInfo, error) {
	if got := cas.HashBytes(data); got != expected {
		return cas.BlobInfo{}, cas.ErrHashMismatch
	}
	if err := s.recordCall(CASStoreCall{Op: "put", Hash: expected, Size: int64(len(data))}); err != nil {
		return cas.BlobInfo{}, err
	}
	if s.Err != nil {
		return cas.BlobInfo{}, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Blobs[expected] = append([]byte(nil), data...)
	if _, ok := s.Provenances[expected]; !ok {
		s.Provenances[expected] = cloneProvenance(prov)
	}
	return cas.BlobInfo{Hash: expected, Size: int64(len(data))}, nil
}

func (s *CASStoreRecorder) Get(ctx context.Context, h cas.Hash) (io.ReadCloser, error) {
	_ = s.recordCall(CASStoreCall{Op: "get", Hash: h})
	if s.Err != nil {
		return nil, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Blobs[h]
	if !ok {
		return nil, cas.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), data...))), nil
}

func (s *CASStoreRecorder) Stat(ctx context.Context, h cas.Hash) (cas.BlobInfo, error) {
	_ = s.recordCall(CASStoreCall{Op: "stat", Hash: h})
	if s.Err != nil {
		return cas.BlobInfo{}, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.Blobs[h]
	if !ok {
		return cas.BlobInfo{}, cas.ErrNotFound
	}
	return cas.BlobInfo{Hash: h, Size: int64(len(data))}, nil
}

func (s *CASStoreRecorder) Has(ctx context.Context, h cas.Hash) (bool, error) {
	_ = s.recordCall(CASStoreCall{Op: "has", Hash: h})
	if s.Err != nil {
		return false, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.Blobs[h]
	return ok, nil
}

func (s *CASStoreRecorder) Provenance(ctx context.Context, h cas.Hash) (cas.Provenance, error) {
	_ = s.recordCall(CASStoreCall{Op: "provenance", Hash: h})
	if s.Err != nil {
		return cas.Provenance{}, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prov, ok := s.Provenances[h]
	if !ok {
		return cas.Provenance{}, cas.ErrNotFound
	}
	return cloneProvenance(prov), nil
}

func (s *CASStoreRecorder) PutActionResult(ctx context.Context, result cas.ActionResult) error {
	if err := s.recordCall(CASStoreCall{Op: "put-action-result", ActionHash: result.ActionHash}); err != nil {
		return err
	}
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Results[result.ActionHash] = cloneActionResult(result)
	return nil
}

func (s *CASStoreRecorder) GetActionResult(ctx context.Context, actionHash cas.Hash) (cas.ActionResult, error) {
	_ = s.recordCall(CASStoreCall{Op: "get-action-result", ActionHash: actionHash})
	if s.Err != nil {
		return cas.ActionResult{}, s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.Results[actionHash]
	if !ok {
		return cas.ActionResult{}, cas.ErrNotFound
	}
	return cloneActionResult(result), nil
}

func (s *CASStoreRecorder) CallsSnapshot() []CASStoreCall {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CASStoreCall(nil), s.Calls...)
}

func (s *CASStoreRecorder) recordCall(call CASStoreCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, call)
	return nil
}

func cloneActionResult(result cas.ActionResult) cas.ActionResult {
	result.Outputs = append([]cas.NamedOutput(nil), result.Outputs...)
	return result
}

func cloneProvenance(prov cas.Provenance) cas.Provenance {
	prov.Source = cloneSource(prov.Source)
	if prov.Attributes != nil {
		attributes := make(map[string]string, len(prov.Attributes))
		for key, value := range prov.Attributes {
			attributes[key] = value
		}
		prov.Attributes = attributes
	}
	return prov
}

func cloneSource(source cas.Source) cas.Source {
	if source.Download != nil {
		download := *source.Download
		source.Download = &download
	}
	if source.Transform != nil {
		transform := *source.Transform
		transform.Inputs = append([]cas.TransformInput(nil), source.Transform.Inputs...)
		source.Transform = &transform
	}
	if source.Import != nil {
		importSource := *source.Import
		source.Import = &importSource
	}
	return source
}
