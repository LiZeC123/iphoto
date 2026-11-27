# 第三方许可与声明

iPhoto 以单二进制分发时包含以下第三方代码。它们都是纯 Go 实现，不引入 CGO。

## github.com/gen2brain/gav1d（AVIF/AV1 解码）

- 用途：解码 AVIF 图片（本项目中只使用解码器）。
- 许可：BSD-2-Clause（下附原文）。
- 专利：这是 AV1 编解码器的实现，不附带任何 AV1 专利授权；AV1 专利许可见
  <https://github.com/gen2brain/gav1d/blob/main/PATENTS>。

```text
Copyright © 2018-2025, VideoLAN and dav1d authors
Copyright (c) 2016, Alliance for Open Media
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## github.com/gen2brain/h265（HEVC/HEIC 解码）

- 用途：解码 HEIC/HEIF 图片（本项目中只使用解码器）。
- 许可：MIT（下附原文）。
- 专利：这是 HEVC 编解码器的实现，不附带任何 HEVC 专利授权。HEVC 涉及多个专利池，
  分发或商用前请自行评估许可。

```text
MIT License

Copyright (c) 2025 roticv
Copyright (c) 2026 Karpeles Lab Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 其它依赖

`modernc.org/sqlite`（BSD-3-Clause）、`disintegration/imaging`（MIT）、
`rwcarlsen/goexif`（BSD-2-Clause）、`golang.org/x/image`（BSD-3-Clause）等
Go 模块的许可随模块源码分发，详见各自仓库。
