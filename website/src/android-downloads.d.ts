declare module 'virtual:android-downloads' {
  const release: {
    version: string
    variants: Array<{ abi: string; filename: string; url: string; checksumUrl: string; bytes: number; sha256: string }>
  }
  export default release
}
