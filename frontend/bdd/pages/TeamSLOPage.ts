import type { Locator, Page } from '@playwright/test'

export class TeamSLOPage {
  readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async goto(team: string) {
    await this.page.goto(`/team/${encodeURIComponent(team)}`)
  }

  heading(team: string): Locator {
    return this.page.getByText(`${team} Dashboard`)
  }

  addButton(): Locator {
    return this.page.getByRole('button', { name: 'Add payload' })
  }

  editButton(): Locator {
    return this.page.getByRole('button', { name: 'Edit' })
  }
}
