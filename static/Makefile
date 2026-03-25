.PHONY: all icons css

all: icons css

icons:
	cd fa && ve/bin/python3 minimize.py icons.spec --url /static/fa/icons.woff2

css:
	lessc --clean-css style.less style.css
