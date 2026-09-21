outline for plans for a "mobile" version of giverny

* it should be a PWA (progressive web app) version of giverny that works on iOS and Android
* it should be anchored at /mobile/
* if we need slightly different harnesses for each one, they can go in `/mobile/ios` and `/mobile/android`
* they should share as much code, design, functionality as possible; they should work the same
* key feature desiderata: background notifications, mobile oriented UI
* potential offline support? queue up modifications and send them when the user is back online? maybe v2
* opening the app presents with full screen login unless the user is logged in already
* can we integrate with face ID/thumbprint? perhaps with a passkey? Logging in should be dead simple
* top of the app has hamburger menu LHS, search bar centered, user icon RHS
  * hamburger slides open left side menu similar to main site; 'my tasks', 'subscribed', 'in progress', and the 'boards' list
  * search inactive for now, perhaps leave out the search element, but eventually it will do what the site search does
  * user icon slides open right side user prefs, which is a full screen element; the user icon's location is replaced with an X which
    slides that drawer back out to the right. It should contain the '/user/settings/' controls as well as the a night/day mode toggle and
    a logout button
* the 'home' page of the app is a scrollable list of boards; these should be sorted the same as they are on the normal home page
* the `/board/name-slug` board detail should present columns of cards, but each column fills most of the horizontal space of the app. If
  there is a column to the left or the right, there should be a vague visual indication (eg the gradient faded ~10-15 left/right most
  pixels of that column are visible as an indicator). The user can swipe left/right to view different columns. drag and drop for cards is
  only there to reorder cards within a column
* tapping a card shows the card detail. we can start by orienting the current 'quick controls' underneath the left-side content, and have
  them all take up the full screen. The 'X' control on the top right brings us back to the previous view.
* all views/edits should use existing websocket paradigm and messages to update in real time. the mobile view can use a banner to indicate
  that the web socket is disconnected

