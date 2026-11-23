import type { Asset } from '@/api/types'

/** 当前支持视频（MVP 仅 MP4）：按后端写入的 MIME 判断。 */
export function isVideoAsset(asset: Pick<Asset, 'mime'> | null | undefined): boolean {
  return !!asset && asset.mime.toLowerCase().startsWith('video/')
}
