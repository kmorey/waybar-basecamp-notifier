# waybar-basecamp-notifier

Simple module to add a notification icon to your Waybar for Basecamp notifications.

The app uses the cookies file from your Chrome/Chromium profile to authenticate to Basecamp.

## Installation

Download the latest file from the Releases page and add it to your path.

## Configuration

1. Install the custom font: copy the `appicons.ttf` font to `~/.share/fonts`.
```sh
cp extras/fontello/font/appicons.ttf ~/.local/share/fonts/
```

2. Add the Waybar configuration

```json
  modules-center: [ ..., "custom/basecamp" ],
  "custom/basecamp": {
    "format": "{}",
    "return-type": "json",
    "exec": "basecamp-notifier",
    "on-click": "gio launch ~/.local/share/applications/Basecamp.desktop"
  },
```

3. Add the Waybar styles

```css
#custom-basecamp {
  font-family: "appicons";
  font-size: 14pt;
  color: #8be9fd;
}
```

4. Configure environment variables (~/.bashrc, ~/.zshrc, etc)

A few variables are used to configure which cookies are used and which Basecamp account to connect to.
The account ID is required, and can be found in the Basecamp home page URL. (https://3.basecamp.com/#######/projects)
The profile is optional, and defaults to `Default`, but if you use a separate profile for Basecamp, you can set that here (e.g. `Profile 1`)

```sh
export BASECAMP_NOTIFIER_ACCOUNT_ID=1234567
export BASECAMP_NOTIFIER_CHROME_PROFILE="Profile 1"
```

## Development

### Compile

```sh
go build -ldflags="-s -w" basecamp-notifier.go
```
