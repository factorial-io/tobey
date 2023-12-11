package main

type Progress interface {
	Update() error
	Close() error
}

func MustStartProgressFromEnv() Progress {
	return &NoopProgress{}
}

type NoopProgress struct{}

func (p *NoopProgress) Update() error {
	return nil
}

func (p *NoopProgress) Close() error {
	return nil
}
