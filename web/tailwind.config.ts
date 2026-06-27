import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        void:    '#09111f',
        space:   '#0f1e35',
        orbit:   '#162844',
        atmos:   '#1d3454',
        'border-dim':    '#1d3454',
        'border-bright': '#2a4a70',
        blue:    '#00a8ff',
        'blue-dim': 'rgba(0,168,255,0.12)',
        green:   '#00e5a0',
        'green-dim': 'rgba(0,229,160,0.12)',
        amber:   '#ffb800',
        'amber-dim': 'rgba(255,184,0,0.12)',
        danger:  '#ff4560',
        'danger-dim': 'rgba(255,69,96,0.12)',
        't1': '#f0f4f8',
        't2': '#7fa8cc',
        't3': '#3d6285',
      },
      fontFamily: {
        ui:   ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
      borderRadius: {
        sm: '4px',
        md: '8px',
        lg: '12px',
        xl: '20px',
      },
    },
  },
  plugins: [],
} satisfies Config
