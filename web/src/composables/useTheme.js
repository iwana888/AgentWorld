import { ref, watch } from 'vue'

// 全局单例主题状态
const theme = ref(localStorage.getItem('aw-theme') || 'dark')

function apply(t) {
  if (t === 'light') document.documentElement.setAttribute('data-theme', 'light')
  else document.documentElement.removeAttribute('data-theme')
}
apply(theme.value)

watch(theme, t => {
  apply(t)
  localStorage.setItem('aw-theme', t)
})

export function useTheme() {
  const toggle = () => { theme.value = theme.value === 'light' ? 'dark' : 'light' }
  return { theme, toggle }
}
