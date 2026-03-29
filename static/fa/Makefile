.PHONY: all virtualenv clean

all: virtualenv
	ve/bin/python3 minimize.py icons.spec --url /static/fa/icons.woff2

virtualenv:
	@if [ ! -d ve ]; then \
		python3 -m venv ve && ve/bin/pip install -r requirements.txt; \
	fi

clean:
	rm -f icons.css icons.woff2
	rm -rf ve