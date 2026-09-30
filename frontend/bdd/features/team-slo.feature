Feature: Team SLO workspace
  As a dashboard user
  I want to see a team's payload SLO and edit it when I own that team
  So that status and incident context stay on the team page

  Scenario: Summary shows the incident and opens the team workspace
    Given the TRT SLO is available
    When I open the main dashboard
    Then I should see "TRT SLO"
    And I should see "CI payload rejected"
    When I open the TRT SLO summary
    Then I should be on the team page for "TRT"
    And I should see the team heading "TRT Dashboard"
    And I should see "nightly-rejected"
    And I should see "3 Payload Streak"

  Scenario: Team SLO owner sees add and edit
    Given I am logged in as a TRT SLO owner
    When I open the TRT team SLO page
    Then I should see the "Add payload" button
    And I should see the "Edit" button

  Scenario: Component owner does not see SLO edit controls
    Given I am logged in as an admin for "Prow"
    When I open the TRT team SLO page
    Then I should not see the "Add payload" button
    And I should not see the "Edit" button

  Scenario: Logged-out user does not see SLO edit controls
    Given I am not logged in
    When I open the TRT team SLO page
    Then I should not see the "Add payload" button
    And I should not see the "Edit" button

  Scenario: Editing a payload keeps the streak and existing link
    Given I am logged in as a TRT SLO owner
    When I open the TRT team SLO page
    And I edit the payload and save
    Then the payload save keeps recurring_count 3
    And the existing SLO link is not changed
