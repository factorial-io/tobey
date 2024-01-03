package main

// func NewThrottledTransport(reqID uint32, redis *redis.Client) *ThrottledTransport {
//
// 	return &ThrottledTransport{
// 		CrawlRequestID: reqID,
// 		limiter:        redis_rate.NewLimiter(redis),
// 	}
//
// }
//
// type ThrottledTransport struct {
// 	CrawlRequestID uint32
// 	limiter        *redis_rate.Limiter
// }
//
// func (t *ThrottledTransport) RoundTrip(r *http.Request) (*http.Response, error) {
// 	ctx := context.Background() // TODO
//
// 	res, err := t.limiter.Allow(ctx, string(t.CrawlRequestID), redis_rate.PerSecond(1)) // TODO: must rate limit on host not request ID or both?
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	select {
// 	case <-time.After(res.Delay()):
// 		return http.RoundTripper.RoundTrip(r)
// 	case <-r.Context().Done():
// 		res.Cancel()
// 		return nil, r.Context().Err()
// 	}
// }
