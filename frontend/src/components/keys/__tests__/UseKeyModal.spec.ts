import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true)
  })
}))

import UseKeyModal from '../UseKeyModal.vue'

describe('UseKeyModal', () => {
  it('renders updated GPT-5.4 mini/nano names in OpenCode config', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: 'openai'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Mini"')
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Nano"')
  })

  it('shows multi-platform tabs for auto-route keys', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-test',
        baseUrl: 'https://example.com/v1',
        platform: null,
        allowMessagesDispatch: true
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const buttons = wrapper.findAll('button')
    expect(buttons.some((button) => button.text().includes('keys.useKeyModal.cliTabs.claudeCode'))).toBe(true)
    expect(buttons.some((button) => button.text().includes('keys.useKeyModal.cliTabs.codexCli'))).toBe(true)
    expect(buttons.some((button) => button.text().includes('keys.useKeyModal.cliTabs.codexCliWs'))).toBe(true)
    expect(buttons.some((button) => button.text().includes('keys.useKeyModal.cliTabs.geminiCli'))).toBe(true)
    expect(buttons.some((button) => button.text().includes('keys.useKeyModal.cliTabs.opencode'))).toBe(true)

    expect(wrapper.text()).not.toContain('keys.useKeyModal.noGroupTitle')

    const codexTab = buttons.find((button) => button.text().includes('keys.useKeyModal.cliTabs.codexCli'))
    expect(codexTab).toBeDefined()
    await codexTab!.trigger('click')
    await nextTick()

    expect(wrapper.find('pre code').text()).toContain('model_provider = "OpenAI"')
  })
})
