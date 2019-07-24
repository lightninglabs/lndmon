FROM alpine

COPY ./script/set_default_graf_dash.sh /bin/

RUN apk --update add curl jq && chmod +x /bin/set_default_graf_dash.sh
CMD set_default_graf_dash.sh