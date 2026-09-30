Feature: Navigation
  As a dashboard user
  I want to navigate between pages using the header menu and links
  So that I can access different views of the dashboard

  Scenario: Clicking a component Details button navigates to component details
    Given I am on the main dashboard
    When I click the "Details" button on the "Prow" component
    Then I should be on the "prow" component details page

  Scenario: Clicking a sub-component card navigates to its details
    Given I am on the main dashboard
    When I click the "Tide" sub-component card in the "Prow" component
    Then I should be on the "prow/tide" sub-component page

  Scenario: Navigate to status history page
    Given I am on the main dashboard
    When I navigate to "Incident History" via the header menu
    Then I should be on the status history page

  Scenario: Navigate to tag-filtered view
    Given I am on the main dashboard
    When I click the "ci" tag
    Then I should be on the tag page for "ci"

  Scenario: Status history page shows component history
    Given I am on the main dashboard
    When I navigate to "Incident History" via the header menu
    Then I should see incident history for "Prow"
    And I should see incident history for "Build Farm"
    And I should see incident history for "Sippy"

  Scenario: Clicking a team chip navigates to team page with filtered results
    Given I am on the main dashboard
    When I click the "TRT" team chip
    Then I should be on the team page for "TRT"
    And I should see the team heading "TRT Dashboard"

  Scenario: Browser back navigation works
    Given I am on the main dashboard
    When I click the "Details" button on the "Prow" component
    And I navigate back
    Then I should be on the main dashboard page
