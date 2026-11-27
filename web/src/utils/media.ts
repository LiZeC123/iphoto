import type { Asset } from '@/api/types'

/** 当前支持视频（MVP 仅 MP4）：按后端写入的 MIME 判断。 */
export function isVideoAsset(asset: Pick<Asset, 'mime'> | null | undefined): boolean {
  return !!asset && asset.mime.toLowerCase().startsWith('video/')
}

/**
 * 浏览器无法直接显示的图片格式：查看器改读 /api/assets/{hash}/preview 的 JPEG 预览。
 * AVIF 主流浏览器已原生支持，不走预览。
 */
export function needsDisplayProxy(asset: Pick<Asset, 'mime'> | null | undefined): boolean {
  const mime = (asset?.mime ?? '').toLowerCase()
  return mime === 'image/tiff' || mime === 'image/heic' || mime === 'image/heif'
}
