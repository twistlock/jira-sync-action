# jira-sync-action
Enable synching Github issues to JIRA project
```
name: Push to JIRA
on:
 issues:
 issue_comment:

jobs:
  sync:
    runs-on: ubuntu-latest
    if: contains( toJson(github), 'CREATE-IN-JIRA' ) # Can use any custom tag to ensure only partial set of issues are synced
    steps:
      - uses: twistlock/jira-sync-action@master
        with:
          JIRA_PROJECTKEY:  Project key
          JIRA_URL:         https://path.to.jira
          JIRA_USEREMAIL:   ${{ secrets.JIRA_USEREMAIL }}
          JIRA_APITOKEN:    ${{ secrets.JIRA_APITOKEN }}
          GITHUB_TOKEN:     ${{ secrets.GITHUB_TOKEN }} # Automatically filled by GH
```
