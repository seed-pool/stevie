import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { applyTheme, getStoredTheme } from './theme'
import './index.css'

applyTheme(getStoredTheme())

/** Block file: URLs from being assigned to DOM nodes (Firefox logs Security Error otherwise). */
function installFileURLGuard() {
  if (typeof window === 'undefined') return
  const block = (value: unknown) => {
    const s = String(value ?? '')
    return /^\s*file:/i.test(s) || /^\s*\/(?:home|Users|var|tmp)\//i.test(s)
  }
  const patch = (proto: { prototype: object }, prop: 'src' | 'href') => {
    const desc = Object.getOwnPropertyDescriptor(proto.prototype, prop)
    if (!desc?.set || !desc.get) return
    Object.defineProperty(proto.prototype, prop, {
      configurable: true,
      enumerable: desc.enumerable,
      get: desc.get,
      set(this: Element, v: string) {
        if (block(v)) return
        desc.set!.call(this, v)
      },
    })
  }
  patch(HTMLImageElement, 'src')
  patch(HTMLScriptElement, 'src')
  patch(HTMLSourceElement, 'src')
  patch(HTMLVideoElement, 'src')
  patch(HTMLAudioElement, 'src')
  patch(HTMLIFrameElement, 'src')
  patch(HTMLAnchorElement, 'href')
  const setAttr = Element.prototype.setAttribute
  Element.prototype.setAttribute = function (name: string, value: string) {
    if ((name === 'src' || name === 'href') && block(value)) return
    return setAttr.call(this, name, value)
  }
  const open = window.open.bind(window)
  window.open = ((url?: string | URL, ...rest: unknown[]) => {
    if (url != null && block(url)) return null
    return open(url as string, ...(rest as [string?, string?]))
  }) as typeof window.open
}

installFileURLGuard()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
