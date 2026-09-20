.PHONY: all icons css

all: icons css

icons:
	$(MAKE) -C fa

css:
	lessc --clean-css style.less style.css
	lessc --clean-css mobile.less mobile.css
