/**
 * 前端密码哈希工具
 * 使用 Web Crypto API 的 SHA-256 对密码进行预哈希
 * 服务端存储 bcrypt(SHA256(plaintext))，明文永远不离开浏览器
 */

/**
 * 计算字符串的 SHA-256 哈希（十六进制）
 * @param {string} text - 原始文本
 * @returns {Promise<string>} 64位十六进制哈希
 */
export async function sha256(text) {
  const encoder = new TextEncoder()
  const data = encoder.encode(text)
  const hashBuffer = await crypto.subtle.digest('SHA-256', data)
  const hashArray = Array.from(new Uint8Array(hashBuffer))
  return hashArray.map(b => b.toString(16).padStart(2, '0')).join('')
}

/**
 * 对密码进行哈希处理
 * @param {string} password - 原始密码
 * @returns {Promise<string>} SHA-256 哈希后的密码
 */
export async function hashPassword(password) {
  return sha256(password)
}
