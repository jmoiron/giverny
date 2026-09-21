/*
 * Shared card-detail boundary.
 *
 * Desktop's current implementation registers its event applier here, while
 * mobile consumes the same public API. Keeping this boundary independent of
 * the board shell lets the card-detail implementation move out of kanban.js
 * without creating a second mobile implementation.
 */
(function () {
    'use strict';

    var implementation = null;
    window.CardDetail = {
        register: function (handler) {
            implementation = handler;
        },
        applyEvent: function (event) {
            if (implementation) return implementation(event);
        }
    };
}());
