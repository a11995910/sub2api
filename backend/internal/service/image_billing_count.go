package service

// resolveBillableImageCount 只使用实际返回的图片数量；请求 n 和分组开关
// 均不能作为出图成功的证据，零张结果不估算收费。保留参数以兼容上游调用。
func resolveBillableImageCount(reported, _ int, _ bool) int {
	return max(reported, 0)
}
