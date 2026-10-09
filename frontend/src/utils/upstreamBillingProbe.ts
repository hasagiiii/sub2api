import { getPlatformSpec } from '@/constants/platformCatalog'

/**
 * 上游倍率探测（upstream billing probe）资格判定。
 *
 * 该探测请求的是 `/v1/sub2api/billing` —— 一个 sub2api 站点之间的 key 级约定端点，
 * 只有「上游本身也是 sub2api 部署」时才会应答。因此资格被限定为文本平台的 API-key 账号：
 *
 *   - 平台须为平台清单中的具体平台，且不是 fork 扩展平台（Kiro 与媒体平台）；
 *   - 账号类型必须是 apikey（OAuth / Bedrock 无静态 Key 可出示）。
 *
 * 媒体类平台（fal / atlascloud / apiz / higgsfield / ByteDance 等）不使用这个中转站探测协议，
 * 不会实现该端点，探测只会把账号密钥发到一个必然 404 的路径，故一律不合格。
 *
 * 与后端 service.IsUpstreamBillingProbeIdentity（domain.IsRequestTargetPlatform）一致。
 */
export function supportsUpstreamBillingProbe(
  platform: string | null | undefined,
  accountType: string | null | undefined
): boolean {
  if (accountType !== 'apikey') return false
  const spec = getPlatformSpec(platform)
  return !!spec && !spec.extension
}
