import { expect } from '@playwright/test'
import { createBdd } from 'playwright-bdd'

import { installTRTSLOMocks, json, PROTECTED, setupApiMocks } from '../fixtures/apiMocks'
import { mockTRTPayloadItem, mockTRTSLOUser } from '../fixtures/mockData'
import { DashboardPage } from '../pages/DashboardPage'
import { TeamSLOPage } from '../pages/TeamSLOPage'

const { Given, When, Then } = createBdd()

let savedBodies: Array<{ details?: { jobs?: Array<{ recurring_count?: number }> } }> = []
let linkCalls: string[] = []

Given('the TRT SLO is available', async ({ page }) => {
  await setupApiMocks(page)
  await installTRTSLOMocks(page)
})

Given('I am logged in as a TRT SLO owner', async ({ page }) => {
  await setupApiMocks(page, { authenticated: true })
  await page.route(`${PROTECTED}/api/user`, (route) => json(route, mockTRTSLOUser))
})

When('I open the main dashboard', async ({ page }) => {
  const dashboard = new DashboardPage(page)
  await dashboard.goto()
  await dashboard.heading.waitFor()
})

When('I open the TRT SLO summary', async ({ page }) => {
  await page.getByText('TRT SLO', { exact: true }).click()
})

When('I open the TRT team SLO page', async ({ page }) => {
  await installTRTSLOMocks(page)
  const userResponse = page.waitForResponse((response) => response.url().includes('/api/user'))
  const teamPage = new TeamSLOPage(page)
  await teamPage.goto('TRT')
  await userResponse
  await teamPage.heading('TRT').waitFor()
})

When('I edit the payload and save', async ({ page }) => {
  savedBodies = []
  linkCalls = []
  await page.route(`${PROTECTED}/api/teams/**`, async (route) => {
    const url = route.request().url()
    const method = route.request().method()
    if (method === 'PUT' && url.endsWith('/slo/items')) {
      savedBodies.push(route.request().postDataJSON())
      return json(route, mockTRTPayloadItem)
    }
    linkCalls.push(`${method} ${url}`)
    return json(route, { error: 'unexpected' }, 500)
  })

  await page.getByRole('button', { name: 'Edit' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.waitFor()
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toBeHidden()
})

Then('I should see {string}', async ({ page }, text: string) => {
  await expect(page.getByText(text, { exact: true }).first()).toBeVisible()
})

Then('the payload save keeps recurring_count 3', async () => {
  expect(savedBodies).toHaveLength(1)
  expect(savedBodies[0].details?.jobs?.[0]?.recurring_count).toBe(3)
})

Then('the existing SLO link is not changed', async () => {
  expect(linkCalls).toEqual([])
})
